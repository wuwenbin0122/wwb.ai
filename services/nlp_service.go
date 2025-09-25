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

type NLPMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type NLPUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type NLPRequest struct {
	Role               models.Role
	Language           string
	History            []NLPMessage
	UserMessage        string
	EnabledSkillIDs    []string
	SummaryThreshold   int
	RecentMessageCount int
	Temperature        float64
	MaxTokens          int
}

type NLPResponse struct {
	Reply           NLPMessage      `json:"reply"`
	Usage           *NLPUsage       `json:"usage,omitempty"`
	Raw             json.RawMessage `json:"raw,omitempty"`
	PromptMessages  []NLPMessage    `json:"prompt_messages"`
	SystemPrompt    string          `json:"system_prompt"`
	HistorySummary  string          `json:"history_summary"`
	EnabledSkillIDs []string        `json:"enabled_skill_ids"`
}

type NLPService struct {
	baseURL string
	model   string
	client  httpDoer
	logger  *zap.SugaredLogger
}

func NewNLPService(cfg *config.Config, logger *zap.SugaredLogger) *NLPService {
	base := strings.TrimRight(cfg.QiniuAPIBaseURL, "/")
	if base == "" {
		base = "https://openai.qiniu.com/v1"
	}

	model := strings.TrimSpace(cfg.QiniuNLPModel)
	if model == "" {
		model = "doubao-1.5-vision-pro"
	}

	return &NLPService{
		baseURL: base,
		model:   model,
		client:  newDefaultHTTPClient(),
		logger:  logger,
	}
}

func (s *NLPService) GenerateReply(ctx context.Context, token string, req NLPRequest) (*NLPResponse, error) {
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

	promptMessages := make([]NLPMessage, 0, 2+len(preservedHistory))
	promptMessages = append(promptMessages, NLPMessage{Role: "system", Content: systemPrompt})
	if historySummary != "" {
		promptMessages = append(promptMessages, NLPMessage{Role: "system", Content: "历史摘要：\n" + historySummary})
	}
	promptMessages = append(promptMessages, preservedHistory...)
	promptMessages = append(promptMessages, NLPMessage{Role: "user", Content: userInput})

	requestPayload := nlpAPIRequest{
		Model:    s.model,
		Messages: promptMessages,
	}
	if req.Temperature > 0 {
		requestPayload.Temperature = req.Temperature
	}
	if req.MaxTokens > 0 {
		requestPayload.MaxTokens = req.MaxTokens
	}

	body, err := json.Marshal(requestPayload)
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

	var apiResp nlpAPIResponse
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

	result := &NLPResponse{
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

func splitHistory(history []NLPMessage, threshold, recentKeep int, assistantName string) (string, []NLPMessage) {
	cleaned := make([]NLPMessage, 0, len(history))
	for _, msg := range history {
		content := strings.TrimSpace(msg.Content)
		role := strings.TrimSpace(msg.Role)
		if content == "" {
			continue
		}
		if role == "" {
			role = "user"
		}
		cleaned = append(cleaned, NLPMessage{Role: role, Content: content})
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
	preserved := append([]NLPMessage(nil), cleaned[summaryCutoff:]...)

	return summary, preserved
}

func summariseMessages(messages []NLPMessage, assistantName string) string {
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
		systemPrompts: []string{"每次回复至少提出 2 个循序渐进的问题，引导对方澄清定义/例外/依据。"},
	},
	"citation_mode": {
		systemPrompts: []string{"若引用，请给出简短来源（作者/著作名/篇章）。无法确定时不要杜撰，提示“可能来源”并告知不确定性。"},
		userRewrite: func(input string) string {
			note := "[请注明出处（作者/著作名/篇章）；不确定时提示可能来源并说明不确定性]"
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
		systemPrompts: []string{"检测到焦虑/沮丧情绪时，先进行共情反映，再给出可执行小步骤。"},
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

type nlpAPIRequest struct {
	Model       string       `json:"model"`
	Messages    []NLPMessage `json:"messages"`
	Temperature float64      `json:"temperature,omitempty"`
	MaxTokens   int          `json:"max_tokens,omitempty"`
}

type nlpAPIChoice struct {
	Index        int        `json:"index"`
	Message      NLPMessage `json:"message"`
	FinishReason string     `json:"finish_reason"`
}

type nlpAPIResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Choices []nlpAPIChoice `json:"choices"`
	Usage   *NLPUsage      `json:"usage"`
	Error   *qiniuAPIError `json:"error,omitempty"`
}
