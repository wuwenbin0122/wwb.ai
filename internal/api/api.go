package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/wuwenbin0122/wwb.ai/internal/auth"
	"github.com/wuwenbin0122/wwb.ai/internal/db"
)

type Handler struct {
	authService *auth.Service
	postgres    *db.Postgres
	mongo       *db.Mongo
}

func NewHandler(authService *auth.Service, postgres *db.Postgres, mongo *db.Mongo) *Handler {
	return &Handler{authService: authService, postgres: postgres, mongo: mongo}
}

func (h *Handler) RegisterRoutes(router *gin.Engine) {
	authGroup := router.Group("/auth")
	authGroup.POST("/register", h.handleRegister)
	authGroup.POST("/login", h.handleLogin)
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
