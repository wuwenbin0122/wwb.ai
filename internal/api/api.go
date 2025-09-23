package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgconn"
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
	roleCreate func(context.Context, roleMutationInput) (*models.Role, error)
	roleUpdate func(context.Context, string, roleMutationInput) (*models.Role, error)
	roleDelete func(context.Context, string) error
}

func NewHandler(authService *auth.Service, postgres *db.Postgres, mongo *db.Mongo) *Handler {
	handler := &Handler{authService: authService, postgres: postgres, mongo: mongo}
	handler.roleLookup = handler.fetchRole
	handler.roleCreate = handler.createRole
	handler.roleUpdate = handler.updateRole
	handler.roleDelete = handler.deleteRole
	return handler
}

func (h *Handler) RegisterRoutes(router *gin.Engine) {
	apiGroup := router.Group("/api")

	authGroup := apiGroup.Group("/auth")
	authGroup.POST("/register", h.handleRegister)
	authGroup.POST("/login", h.handleLogin)

	roleGroup := apiGroup.Group("/role")
	roleGroup.POST("", h.handleRoleCreate)
	roleGroup.PUT(":id", h.handleRoleUpdate)
	roleGroup.DELETE(":id", h.handleRoleDelete)
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

type roleCreateRequest struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type roleUpdateRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type roleMutationInput struct {
	ID          string
	Name        string
	Description string
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

func (h *Handler) handleRoleCreate(c *gin.Context) {
	var req roleCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid payload", err)
		return
	}

	if strings.TrimSpace(req.Name) == "" {
		writeError(c, http.StatusBadRequest, "name is required", errRoleNameRequired)
		return
	}

	ctx := c.Request.Context()
	role, err := h.roleCreate(ctx, roleMutationInput{
		ID:          req.ID,
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		switch {
		case errors.Is(err, errRoleAlreadyExists):
			writeError(c, http.StatusConflict, err.Error(), err)
		case errors.Is(err, errRoleNotConfigured):
			writeError(c, http.StatusInternalServerError, "role store not configured", err)
		default:
			writeError(c, http.StatusInternalServerError, "failed to create role", err)
		}
		return
	}

	c.JSON(http.StatusCreated, roleToResponse(role))
}

func (h *Handler) handleRoleUpdate(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		writeError(c, http.StatusBadRequest, "role id is required", errMissingRoleID)
		return
	}

	var req roleUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid payload", err)
		return
	}

	if strings.TrimSpace(req.Name) == "" {
		writeError(c, http.StatusBadRequest, "name is required", errRoleNameRequired)
		return
	}

	ctx := c.Request.Context()
	role, err := h.roleUpdate(ctx, id, roleMutationInput{
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		switch {
		case errors.Is(err, errRoleNotFound):
			writeError(c, http.StatusNotFound, err.Error(), err)
		case errors.Is(err, errRoleNotConfigured):
			writeError(c, http.StatusInternalServerError, "role store not configured", err)
		default:
			writeError(c, http.StatusInternalServerError, "failed to update role", err)
		}
		return
	}

	c.JSON(http.StatusOK, roleToResponse(role))
}

func (h *Handler) handleRoleDelete(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		writeError(c, http.StatusBadRequest, "role id is required", errMissingRoleID)
		return
	}

	ctx := c.Request.Context()
	if err := h.roleDelete(ctx, id); err != nil {
		switch {
		case errors.Is(err, errRoleNotFound):
			writeError(c, http.StatusNotFound, err.Error(), err)
		case errors.Is(err, errRoleNotConfigured):
			writeError(c, http.StatusInternalServerError, "role store not configured", err)
		default:
			writeError(c, http.StatusInternalServerError, "failed to delete role", err)
		}
		return
	}

	c.Status(http.StatusNoContent)
}

var (
	errMissingRoleID     = errors.New("roleId is required")
	errRoleNotFound      = errors.New("role not found")
	errRoleNotConfigured = errors.New("role backend not configured")
	errRoleAlreadyExists = errors.New("role already exists")
	errRoleNameRequired  = errors.New("name is required")
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

func roleToResponse(role *models.Role) gin.H {
	return gin.H{
		"id":          role.ID,
		"name":        role.Name,
		"description": role.Description,
		"createdAt":   role.CreatedAt.Format(time.RFC3339),
	}
}

func (h *Handler) createRole(ctx context.Context, input roleMutationInput) (*models.Role, error) {
	if h.postgres == nil || h.postgres.Pool == nil {
		return nil, errRoleNotConfigured
	}

	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = uuid.NewString()
	}

	now := time.Now().UTC()
	_, err := h.postgres.Pool.Exec(ctx,
		`INSERT INTO roles (id, name, description, created_at) VALUES ($1, $2, $3, $4)`,
		id, input.Name, input.Description, now,
	)
	if err != nil {
		if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "23505" {
			return nil, errRoleAlreadyExists
		}
		return nil, err
	}

	role := &models.Role{
		ID:          id,
		Name:        input.Name,
		Description: input.Description,
		CreatedAt:   now,
	}

	if h.mongo != nil && h.mongo.Roles != nil {
		_, mongoErr := h.mongo.Roles.InsertOne(ctx, bson.M{
			"_id":         role.ID,
			"name":        role.Name,
			"description": role.Description,
			"created_at":  role.CreatedAt,
		})
		if mongoErr != nil && !mongo.IsDuplicateKeyError(mongoErr) {
			return nil, mongoErr
		}
	}

	return role, nil
}

func (h *Handler) updateRole(ctx context.Context, id string, input roleMutationInput) (*models.Role, error) {
	if h.postgres == nil || h.postgres.Pool == nil {
		return nil, errRoleNotConfigured
	}

	var createdAt time.Time
	err := h.postgres.Pool.QueryRow(ctx,
		`UPDATE roles SET name = $1, description = $2 WHERE id = $3 RETURNING created_at`,
		input.Name, input.Description, id,
	).Scan(&createdAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errRoleNotFound
		}
		return nil, err
	}

	if h.mongo != nil && h.mongo.Roles != nil {
		_, mongoErr := h.mongo.Roles.UpdateOne(ctx,
			bson.M{"_id": id},
			bson.M{"$set": bson.M{"name": input.Name, "description": input.Description}},
		)
		if mongoErr != nil && !errors.Is(mongoErr, mongo.ErrNoDocuments) {
			return nil, mongoErr
		}
	}

	return &models.Role{
		ID:          id,
		Name:        input.Name,
		Description: input.Description,
		CreatedAt:   createdAt,
	}, nil
}

func (h *Handler) deleteRole(ctx context.Context, id string) error {
	if h.postgres == nil || h.postgres.Pool == nil {
		return errRoleNotConfigured
	}

	commandTag, err := h.postgres.Pool.Exec(ctx, `DELETE FROM roles WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if commandTag.RowsAffected() == 0 {
		return errRoleNotFound
	}

	if h.mongo != nil && h.mongo.Roles != nil {
		_, mongoErr := h.mongo.Roles.DeleteOne(ctx, bson.M{"_id": id})
		if mongoErr != nil && !errors.Is(mongoErr, mongo.ErrNoDocuments) {
			return mongoErr
		}
	}

	return nil
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
