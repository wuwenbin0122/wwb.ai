package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"github.com/wuwenbin0122/wwb.ai/db/models"
	"gorm.io/gorm"
)

// RoleHandler provides HTTP handlers for role resources.
type RoleHandler struct {
	db *gorm.DB
}

// NewRoleHandler constructs a RoleHandler backed by the provided gorm database connection.
func NewRoleHandler(db *gorm.DB) *RoleHandler {
	return &RoleHandler{db: db}
}

type roleListResponse struct {
	Data       []models.Role `json:"data"`
	Pagination pagination    `json:"pagination"`
}

type pagination struct {
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
	Total    int64 `json:"total"`
}

// GetRoles responds with roles filtered by optional domain, search, or tags parameters and supports pagination.
func (h *RoleHandler) GetRoles(c *gin.Context) {
	page := parsePositiveInt(c.Query("page"), 1)
	pageSize := parsePositiveInt(c.Query("page_size"), 12)
	if pageSize > 50 {
		pageSize = 50
	}

	search := strings.TrimSpace(c.Query("search"))
	domain := strings.TrimSpace(c.Query("domain"))
	tags := parseTagTerms(c.Query("tags"))

	query := h.db.WithContext(c.Request.Context()).Model(&models.Role{})

	if domain != "" {
		like := "%" + domain + "%"
		query = query.Where("domain ILIKE ?", like)
	}

	if search != "" {
		like := "%" + search + "%"
		query = query.Where("(name ILIKE ? OR bio ILIKE ?)", like, like)
	}

	if len(tags) > 0 {
		query = query.Where("tags @> ?", pq.StringArray(tags))
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "count roles failed"})
		return
	}

	offset := (page - 1) * pageSize
	roles := make([]models.Role, 0, pageSize)
	if err := query.Order("id ASC").Limit(pageSize).Offset(offset).Find(&roles).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query roles failed"})
		return
	}

	c.JSON(http.StatusOK, roleListResponse{
		Data: roles,
		Pagination: pagination{
			Page:     page,
			PageSize: pageSize,
			Total:    total,
		},
	})
}

func parseTagTerms(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';'
	})

	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}

	return cleaned
}

func parsePositiveInt(raw string, fallback int) int {
	if strings.TrimSpace(raw) == "" {
		return fallback
	}

	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return fallback
	}

	return value
}
