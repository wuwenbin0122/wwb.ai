package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wuwenbin0122/wwb.ai/config"
	"github.com/wuwenbin0122/wwb.ai/db"
	"github.com/wuwenbin0122/wwb.ai/services"
	"go.uber.org/zap"
)

type NLPHandler struct {
	cfg    *config.Config
	pool   *pgxpool.Pool
	nlp    *services.NLPService
	logger *zap.SugaredLogger
}

func NewNLPHandler(cfg *config.Config, pool *pgxpool.Pool, nlp *services.NLPService, logger *zap.SugaredLogger) *NLPHandler {
	return &NLPHandler{cfg: cfg, pool: pool, nlp: nlp, logger: logger}
}

type nlpMessagePayload struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type nlpRequestPayload struct {
	Token             string              `json:"token"`
	RoleID            int64               `json:"role_id"`
	Language          string              `json:"language"`
	Messages          []nlpMessagePayload `json:"messages"`
	EnabledSkillIDs   []string            `json:"enabled_skill_ids"`
	SummaryThreshold  int                 `json:"summary_threshold"`
	RecentMessageKeep int                 `json:"recent_message_keep"`
	Temperature       float64             `json:"temperature"`
	MaxTokens         int                 `json:"max_tokens"`
}

func (h *NLPHandler) HandleChat(c *gin.Context) {
	var payload nlpRequestPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request payload", "detail": err.Error()})
		return
	}

	if payload.RoleID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "role_id is required"})
		return
	}

	messages := normalizeNLPMessages(payload.Messages)
	if len(messages) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "at least one message is required"})
		return
	}

	last := messages[len(messages)-1]
	if strings.ToLower(last.Role) != "user" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "last message must be from user"})
		return
	}

	role, err := db.GetRoleByID(c.Request.Context(), h.pool, payload.RoleID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "role not found"})
			return
		}
		h.logger.Warnf("fetch role failed: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to load role", "detail": err.Error()})
		return
	}

	language := strings.TrimSpace(payload.Language)
	if language == "" && len(role.Languages) > 0 {
		language = strings.TrimSpace(role.Languages[0])
	}

	history := messages[:len(messages)-1]

	req := services.NLPRequest{
		Role:               *role,
		Language:           language,
		History:            history,
		UserMessage:        last.Content,
		EnabledSkillIDs:    payload.EnabledSkillIDs,
		SummaryThreshold:   payload.SummaryThreshold,
		RecentMessageCount: payload.RecentMessageKeep,
		Temperature:        payload.Temperature,
		MaxTokens:          payload.MaxTokens,
	}

	token := h.resolveToken(c, payload.Token)
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "qiniu token is required"})
		return
	}

	result, err := h.nlp.GenerateReply(c.Request.Context(), token, req)
	if err != nil {
		h.logger.Warnf("nlp chat failed: %v", err)
		c.JSON(statusFromError(err), gin.H{"error": "chat completion failed", "detail": err.Error()})
		return
	}

	response := gin.H{
		"message":           result.Reply,
		"usage":             result.Usage,
		"raw":               result.Raw,
		"prompt_messages":   result.PromptMessages,
		"system_prompt":     result.SystemPrompt,
		"history_summary":   result.HistorySummary,
		"enabled_skill_ids": result.EnabledSkillIDs,
	}

	c.JSON(http.StatusOK, response)
}

func normalizeNLPMessages(payload []nlpMessagePayload) []services.NLPMessage {
	result := make([]services.NLPMessage, 0, len(payload))
	for _, msg := range payload {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		role := strings.TrimSpace(msg.Role)
		if role == "" {
			role = "user"
		}
		result = append(result, services.NLPMessage{Role: role, Content: content})
	}
	return result
}

func (h *NLPHandler) resolveToken(c *gin.Context, explicit string) string {
	if token := strings.TrimSpace(explicit); token != "" {
		return token
	}

	if header := parseAuthorizationToken(c.GetHeader("Authorization")); header != "" {
		return header
	}

	return strings.TrimSpace(h.cfg.QiniuAPIKey)
}
