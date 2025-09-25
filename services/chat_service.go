package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/wuwenbin0122/wwb.ai/config"
	"github.com/wuwenbin0122/wwb.ai/db/models"
	"go.uber.org/zap"
)

const (
	defaultSummaryThreshold  = 8
	defaultRecentMessageKeep = 4
	defaultLanguage          = "zh"
	maxSummaryRuneLength     = 120
)

// ChatMessage mirrors OpenAI/Qiniu chat message payloads.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatUsage contains token usage metadata returned by Qiniu's API.
type ChatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatRequest describes a prompt orchestration operation.
type ChatRequest struct {
	Role               models.Role
	Language           string
	History            []ChatMessage
	UserMessage        string
	EnabledSkillIDs    []string
	SummaryThreshold   int
	RecentMessageCount int
	Temperature        float64
	MaxTokens          int
}

// ChatResponse wraps the assistant reply and debug metadata.
type ChatResponse struct {
	Reply           ChatMessage     `json:"reply"`
	Usage           *ChatUsage      `json:"usage,omitempty"`
	Raw             json.RawMessage `json:"raw,omitempty"`
	PromptMessages  []ChatMessage   `json:"prompt_messages"`
	SystemPrompt    string          `json:"system_prompt"`
	HistorySummary  string          `json:"history_summary"`
	EnabledSkillIDs []string        `json:"enabled_skill_ids"`
}

// ChatService handles prompt composition plus Qiniu chat completions.
type ChatService struct {
	baseURL string
	model   string
	client  httpDoer
	logger  *zap.SugaredLogger
}

// NewChatService constructs a ChatService initialized from cfg.
func NewChatService(cfg *config.Config, logger *zap.SugaredLogger) *ChatService {
	base := strings.TrimRight(cfg.QiniuAPIBaseURL, "/")
	if base == "" {
		base = "https://openai.qiniu.com/v1"
	}

	model := strings.TrimSpace(cfg.QiniuNLPModel)
	if model == "" {
		model = "doubao-1.5-vision-pro"
	}

	return &ChatService{
		baseURL: base,
		model:   model,
		client:  newDefaultHTTPClient(),
		logger:  logger,
	}
}

// GenerateReply builds a structured prompt and forwards it to Qiniu's chat completion API.
func (s *ChatService) GenerateReply(ctx context.Context, token string, req ChatRequest) (*ChatResponse, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("authorization token is required")
	}

	userInput := strings.TrimSpace(req.UserMessage)
	if userInput == "" {
		return nil, fmt.Errorf("user message cannot be empty")
	}

	lang := strings.TrimSpace(req.Language)
	if lang == "" {
		lang = defaultLanguage
	}

	summaryThreshold := req.SummaryThreshold
	if summaryThreshold <= 0 {
		summaryThreshold = defaultSummaryThreshold
	}

	recentKeep := req.RecentMessageCount
	if recentKeep <= 0 {
		recentKeep = defaultRecentMessageKeep
	}
	if recentKeep > summaryThreshold {
		recentKeep = summaryThreshold
	}

	persona := decodeRolePersonality(req.Role.Personality)
	roleSkills := decodeRoleSkills(req.Role.Skills)
	skillIndex := make(map[string]roleSkill, len(roleSkills))
	for _, skill := range roleSkills {
		if skill.ID == "" {
			continue
		}
		skillIndex[skill.ID] = skill
	}

	enabledIDs := filterSkillIDs(req.EnabledSkillIDs, skillIndex)
	enabledNames := make([]string, 0, len(enabledIDs))
	for _, id := range enabledIDs {
		enabledNames = append(enabledNames, skillIndex[id].Name)
	}

	enabledCSV := "无"
	if len(enabledNames) > 0 {
		enabledCSV = strings.Join(enabledNames, ", ")
	}

	skillDirectives, rewrittenUser := applySkillHooks(enabledIDs, userInput)
	if rewrittenUser != "" {
		userInput = rewrittenUser
	}

	systemPrompt := buildSystemPrompt(req.Role.Name, persona, strings.TrimSpace(req.Role.Background), enabledCSV, lang, skillDirectives)

	historySummary, preservedHistory := splitHistory(req.History, summaryThreshold, recentKeep, req.Role.Name)

	promptMessages := make([]ChatMessage, 0, 2+len(preservedHistory))
	promptMessages = append(promptMessages, ChatMessage{Role: "system", Content: systemPrompt})
	if historySummary != "" {
		promptMessages = append(promptMessages, ChatMessage{Role: "system", Content: "历史摘要：\n" + historySummary})
	}
	promptMessages = append(promptMessages, preservedHistory...)
	promptMessages = append(promptMessages, ChatMessage{Role: "user", Content: userInput})

	chatPayload := chatAPIRequest{
		Model:    s.model,
		Messages: promptMessages,
	}
	if req.Temperature > 0 {
		chatPayload.Temperature = req.Temperature
	}
	if req.MaxTokens > 0 {
		chatPayload.MaxTokens = req.MaxTokens
	}

	body, err := json.Marshal(chatPayload)
	if err != nil {
		return nil, fmt.Errorf("marshal chat payload: %w", err)
	}

	endpoint := s.baseURL + "/chat/completions"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create chat request: %w", err)
	}

	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")

	response, err := s.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("call chat api: %w", err)
	}
	defer response.Body.Close()

	respBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read chat response: %w", err)
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, buildQiniuAPIError(response.StatusCode, respBody)
	}

	var apiResp chatAPIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("decode chat response: %w", err)
	}

	if apiResp.Error != nil && apiResp.Error.Message != "" {
		return nil, fmt.Errorf("qiniu chat error: %s", apiResp.Error.Message)
	}

	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("chat response contained no choices")
	}

	reply := apiResp.Choices[0].Message
	if strings.TrimSpace(reply.Role) == "" {
		reply.Role = "assistant"
	}

	result := &ChatResponse{
		Reply:           reply,
		Usage:           apiResp.Usage,
		Raw:             json.RawMessage(respBody),
		PromptMessages:  promptMessages,
		SystemPrompt:    systemPrompt,
		HistorySummary:  historySummary,
		EnabledSkillIDs: enabledIDs,
	}

	return result, nil
}

type rolePersonality struct {
	Tone        string   `json:"tone"`
	Style       string   `json:"style"`
	Constraints []string `json:"constraints"`
}

type roleSkill struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func decodeRolePersonality(raw json.RawMessage) rolePersonality {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return rolePersonality{}
	}

	var persona rolePersonality
	if err := json.Unmarshal(trimmed, &persona); err != nil {
		return rolePersonality{}
	}

	return persona
}

func decodeRoleSkills(raw json.RawMessage) []roleSkill {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil
	}

	var skills []roleSkill
	if err := json.Unmarshal(trimmed, &skills); err != nil {
		return nil
	}

	result := make([]roleSkill, 0, len(skills))
	for _, skill := range skills {
		id := strings.TrimSpace(skill.ID)
		name := strings.TrimSpace(skill.Name)
		if id == "" {
			continue
		}
		result = append(result, roleSkill{ID: id, Name: name})
	}

	return result
}

func filterSkillIDs(ids []string, allowed map[string]roleSkill) []string {
	seen := make(map[string]struct{}, len(ids))
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			continue
		}
		if _, ok := allowed[trimmed]; !ok {
			continue
		}
		if _, dup := seen[trimmed]; dup {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func buildSystemPrompt(roleName string, persona rolePersonality, background, enabledCSV, lang string, skillDirectives []string) string {
	if roleName == "" {
		roleName = "角色"
	}
	background = strings.TrimSpace(background)
	if background == "" {
		background = "暂无背景信息"
	}

	tone := strings.TrimSpace(persona.Tone)
	if tone == "" {
		tone = "保持温和与理性"
	}

	style := strings.TrimSpace(persona.Style)
	if style == "" {
		style = "表达清晰、结构化"
	}

	constraints := strings.Join(filterNonEmpty(persona.Constraints), "；")
	if constraints == "" {
		constraints = "无特别约束"
	}

	lang = strings.TrimSpace(lang)
	if lang == "" {
		lang = defaultLanguage
	}

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("你是一名 %s 的拟人化对话体。遵循以下人设：\n", roleName))
	builder.WriteString(fmt.Sprintf("- 背景：%s\n", background))
	builder.WriteString(fmt.Sprintf("- 语气与风格：%s；%s\n", tone, style))
	builder.WriteString(fmt.Sprintf("- 约束：%s\n", constraints))
	builder.WriteString(fmt.Sprintf("- 技能开关：%s\n", enabledCSV))
	builder.WriteString("通用规则：\n")
	builder.WriteString(fmt.Sprintf("- 回答语言：%s\n", lang))
	builder.WriteString("- 尽量分段，必要时项目符号清晰表达。\n")
	builder.WriteString("- 对事实类内容，如不确定请说明不确定并给出进一步追问或验证路径。")

	if len(skillDirectives) > 0 {
		builder.WriteString("\n技能指令：")
		for _, directive := range skillDirectives {
			dir := strings.TrimSpace(directive)
			if dir == "" {
				continue
			}
			builder.WriteString("\n- ")
			builder.WriteString(dir)
		}
	}

	return builder.String()
}

func filterNonEmpty(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		result = append(result, trimmed)
	}
	return result
}

func splitHistory(history []ChatMessage, threshold, recentKeep int, assistantName string) (string, []ChatMessage) {
	cleaned := make([]ChatMessage, 0, len(history))
	for _, msg := range history {
		content := strings.TrimSpace(msg.Content)
		role := strings.TrimSpace(msg.Role)
		if content == "" {
			continue
		}
		if role == "" {
			role = "user"
		}
		cleaned = append(cleaned, ChatMessage{Role: role, Content: content})
	}

	if threshold <= 0 || len(cleaned) <= threshold {
		return "", cleaned
	}

	if recentKeep <= 0 {
		recentKeep = defaultRecentMessageKeep
	}
	if recentKeep >= len(cleaned) {
		recentKeep = len(cleaned)
	}

	summaryCutoff := len(cleaned) - recentKeep
	if summaryCutoff < 0 {
		summaryCutoff = 0
	}

	summary := summariseMessages(cleaned[:summaryCutoff], assistantName)
	preserved := append([]ChatMessage(nil), cleaned[summaryCutoff:]...)

	return summary, preserved
}

func summariseMessages(messages []ChatMessage, assistantName string) string {
	if len(messages) == 0 {
		return ""
	}

	var builder strings.Builder
	index := 1
	for _, msg := range messages {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		roleLabel := labelForRole(msg.Role, assistantName)
		builder.WriteString(fmt.Sprintf("%d. %s：%s\n", index, roleLabel, truncateRunes(content, maxSummaryRuneLength)))
		index++
	}

	return strings.TrimSpace(builder.String())
}

func labelForRole(role, assistantName string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "assistant":
		if strings.TrimSpace(assistantName) != "" {
			return assistantName
		}
		return "助手"
	case "system":
		return "系统"
	case "tool":
		return "工具"
	default:
		return "用户"
	}
}

func truncateRunes(input string, max int) string {
	if max <= 0 {
		return input
	}
	if utf8.RuneCountInString(input) <= max {
		return input
	}

	var builder strings.Builder
	count := 0
	for _, r := range input {
		if count >= max {
			builder.WriteRune('…')
			break
		}
		builder.WriteRune(r)
		count++
	}
	return builder.String()
}

type skillDirective struct {
	systemPrompts []string
	userRewrite   func(string) string
}

var skillHooks = map[string]skillDirective{
	"socratic_questions": {
		systemPrompts: []string{"优先以 2-3 个开放式问题引导用户思考，再给出总结。"},
	},
	"citation_mode": {
		systemPrompts: []string{"引用或引用事实时请在段落末尾简要标注出处来源。"},
		userRewrite: func(input string) string {
			note := "[用户期望答案附带出处说明]"
			if strings.Contains(input, note) {
				return input
			}
			if strings.TrimSpace(input) == "" {
				return input
			}
			return strings.TrimSpace(input) + "\n\n" + note
		},
	},
	"emo_stabilizer": {
		systemPrompts: []string{"保持情绪稳定且具备同理心，必要时先回应用户情绪再给出建议。"},
	},
}

func applySkillHooks(enabledIDs []string, userInput string) ([]string, string) {
	directives := make([]string, 0, len(enabledIDs))
	modified := userInput
	for _, id := range enabledIDs {
		hook, ok := skillHooks[id]
		if !ok {
			continue
		}
		directives = append(directives, hook.systemPrompts...)
		if hook.userRewrite != nil {
			modified = hook.userRewrite(modified)
		}
	}
	return filterNonEmpty(directives), modified
}

type chatAPIRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Temperature float64       `json:"temperature,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
}

type chatAPIChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type chatAPIResponse struct {
	ID      string          `json:"id"`
	Object  string          `json:"object"`
	Created int64           `json:"created"`
	Choices []chatAPIChoice `json:"choices"`
	Usage   *ChatUsage      `json:"usage"`
	Error   *qiniuAPIError  `json:"error,omitempty"`
}
