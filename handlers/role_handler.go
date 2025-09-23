package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wuwenbin0122/wwb.ai/db/models"
)

// RoleHandler provides HTTP handlers for role resources.
type RoleHandler struct {
	pool *pgxpool.Pool
}

func NewRoleHandler(pool *pgxpool.Pool) *RoleHandler {
	return &RoleHandler{pool: pool}
}

// GetRoles responds with roles filtered by optional domain or tags query parameters.
func (h *RoleHandler) GetRoles(c *gin.Context) {
	domain := strings.TrimSpace(c.Query("domain"))
	tagsParam := strings.TrimSpace(c.Query("tags"))

	baseQuery := `SELECT id, name, domain, tags, bio FROM roles`
	clauses := make([]string, 0, 2)
	args := make([]interface{}, 0, 3)

	if domain != "" {
		clauses = append(clauses, fmt.Sprintf("domain = $%d", len(args)+1))
		args = append(args, domain)
	}

	if tagsParam != "" {
		tagTerms := parseTagTerms(tagsParam)
		tagClauses := make([]string, 0, len(tagTerms))

		for _, tag := range tagTerms {
			if tag == "" {
				continue
			}

			tagClauses = append(tagClauses, fmt.Sprintf("tags ILIKE '%%' || $%d || '%%'", len(args)+1))
			args = append(args, tag)
		}

		if len(tagClauses) > 0 {
			clauses = append(clauses, "("+strings.Join(tagClauses, " OR ")+")")
		}
	}

	query := baseQuery
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY id"

	rows, err := h.pool.Query(c.Request.Context(), query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query roles failed"})
		return
	}
	defer rows.Close()

	roles := make([]models.Role, 0)
	for rows.Next() {
		var role models.Role
		if err := rows.Scan(&role.ID, &role.Name, &role.Domain, &role.Tags, &role.Bio); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "scan role failed"})
			return
		}
		roles = append(roles, role)
	}

	if rows.Err() != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "iterate roles failed"})
		return
	}

	c.JSON(http.StatusOK, roles)
}

func parseTagTerms(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';'
	})

	for i, part := range parts {
		parts[i] = strings.TrimSpace(part)
	}

	return parts
}
