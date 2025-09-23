package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/wuwenbin0122/wwb.ai/internal/auth"
	"github.com/wuwenbin0122/wwb.ai/internal/db"
	"github.com/wuwenbin0122/wwb.ai/internal/models"
)

type Handler struct {
	authService *auth.Service
	postgres    *db.Postgres
	mongo       *db.Mongo

	roleLookup func(context.Context, string) (*models.Role, error)
}

func NewHandler(authService *auth.Service, postgres *db.Postgres, mongo *db.Mongo) *Handler {
	return &Handler{authService: authService, postgres: postgres, mongo: mongo}
}

func (h *Handler) RegisterRoutes(router *gin.Engine) {
	apiGroup := router.Group("/api")

	authGroup := apiGroup.Group("/auth")
	authGroup.POST("/register", h.handleRegister)
	authGroup.POST("/login", h.handleLogin)

	roleGroup := apiGroup.Group("/role")
	roleGroup.POST("/select", h.handleRoleSelect)
}

type registerRequest struct {
	Username string
	Email    string
	Password string
}

type loginRequest struct {
	Identifier string
	Password   string
}

type selectRoleRequest struct {
	RoleID string
}

func (h *Handler) handleRegister(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid payload", err)
		return
	}

	result, err := h.authService.Register(c.Request.Context(), auth.RegisterInput{
		Username: req.Username,
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		switch err {
		case auth.ErrUsernameRequired, auth.ErrPasswordTooWeak:
			writeError(c, http.StatusBadRequest, err.Error(), err)
			return
		case auth.ErrUserExists, auth.ErrEmailExists:
			writeError(c, http.StatusConflict, err.Error(), err)
			return
		default:
			writeError(c, http.StatusInternalServerError, "failed to register user", err)
			return
		}
	}

	c.JSON(http.StatusCreated, newAuthResponse(result))
}

func (h *Handler) handleLogin(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid payload", err)
		return
	}

	if req.Identifier == "" || req.Password == "" {
		writeError(c, http.StatusBadRequest, "identifier and password are required", auth.ErrInvalidCredentials)
		return
	}

	result, err := h.authService.Login(c.Request.Context(), auth.LoginInput{
		Identifier: req.Identifier,
		Password:   req.Password,
	})
	if err != nil {
		switch err {
		case auth.ErrInvalidCredentials:
			writeError(c, http.StatusUnauthorized, err.Error(), err)
			return
		default:
			writeError(c, http.StatusInternalServerError, "failed to login", err)
			return
		}
	}

	c.JSON(http.StatusOK, newAuthResponse(result))
}

func (h *Handler) handleRoleSelect(c *gin.Context) {
	var req selectRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid payload", err)
		return
	}

	if req.RoleID == "" {
		writeError(c, http.StatusBadRequest, "roleId is required", errMissingRoleID)
		return
	}

	ctx := c.Request.Context()
	roleFetcher := h.fetchRole
	if h.roleLookup != nil {
		roleFetcher = h.roleLookup
	}

	role, err := roleFetcher(ctx, req.RoleID)
	if err != nil {
		switch {
		case errors.Is(err, errRoleNotFound):
			writeError(c, http.StatusNotFound, err.Error(), err)
		default:
			writeError(c, http.StatusInternalServerError, "failed to select role", err)
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":          role.ID,
		"name":        role.Name,
		"description": role.Description,
		"createdAt":   role.CreatedAt.Format(time.RFC3339),
	})
}

var (
	errMissingRoleID = errors.New("roleId is required")
	errRoleNotFound  = errors.New("role not found")
)

func (h *Handler) fetchRole(ctx context.Context, roleID string) (*models.Role, error) {
	if h.postgres != nil && h.postgres.Pool != nil {
		var role models.Role
		query := "SELECT id, name, description, created_at FROM roles WHERE id = $1"
		if err := h.postgres.Pool.QueryRow(ctx, query, roleID).Scan(&role.ID, &role.Name, &role.Description, &role.CreatedAt); err == nil {
			return &role, nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("postgres query role: %w", err)
		}
	}

	if h.mongo != nil && h.mongo.Roles != nil {
		var doc bson.M
		filter := bson.M{"_id": roleID}
		if err := h.mongo.Roles.FindOne(ctx, filter).Decode(&doc); err == nil {
			return roleFromBSON(doc), nil
		} else if !errors.Is(err, mongo.ErrNoDocuments) {
			return nil, fmt.Errorf("mongo query role: %w", err)
		}
	}

	return nil, errRoleNotFound
}

func roleFromBSON(doc bson.M) *models.Role {
	role := &models.Role{}
	if id, ok := doc["_id"].(string); ok {
		role.ID = id
	} else {
		role.ID = fmt.Sprint(doc["_id"])
	}

	if name, ok := doc["name"].(string); ok {
		role.Name = name
	}

	if desc, ok := doc["description"].(string); ok {
		role.Description = desc
	}

	switch v := doc["created_at"].(type) {
	case time.Time:
		role.CreatedAt = v
	case primitive.DateTime:
		role.CreatedAt = v.Time()
	default:
		role.CreatedAt = time.Now().UTC()
	}

	return role
}

func newAuthResponse(result *auth.AuthResult) gin.H {
	return gin.H{
		"token":     result.Token,
		"expiresAt": result.ExpiresAt.Format(time.RFC3339),
		"user": gin.H{
			"id":        result.User.ID,
			"username":  result.User.Username,
			"email":     result.User.Email,
			"createdAt": result.User.CreatedAt.Format(time.RFC3339),
			"updatedAt": result.User.UpdatedAt.Format(time.RFC3339),
		},
	}
}

func writeError(c *gin.Context, status int, message string, err error) {
	c.JSON(status, gin.H{
		"error":   message,
		"details": err.Error(),
	})
}
