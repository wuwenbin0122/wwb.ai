package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/wuwenbin0122/wwb.ai/internal/auth"
	"github.com/wuwenbin0122/wwb.ai/internal/models"
)

func setupTestRouter(t *testing.T) (*gin.Engine, *Handler) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	authService, err := auth.NewService("test-secret", time.Hour)
	if err != nil {
		t.Fatalf("failed to create auth service: %v", err)
	}

	handler := NewHandler(authService, nil, nil)
	router := gin.New()
	handler.RegisterRoutes(router)

	return router, handler
}

func TestAuthRegisterAndLogin(t *testing.T) {
	router, _ := setupTestRouter(t)

	registerBody := map[string]string{
		"username": "alice",
		"email":    "alice@example.com",
		"password": "secret123",
	}

	rec := httptest.NewRecorder()
	req := newJSONRequest(t, http.MethodPost, "/api/auth/register", registerBody)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", rec.Code)
	}

	var registerResp map[string]any
	decodeBody(t, rec.Body.Bytes(), &registerResp)
	if registerResp["token"] == "" {
		t.Fatalf("expected token in registration response")
	}

	loginBody := map[string]string{
		"identifier": "alice",
		"password":   "secret123",
	}

	rec = httptest.NewRecorder()
	req = newJSONRequest(t, http.MethodPost, "/api/auth/login", loginBody)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var loginResp map[string]any
	decodeBody(t, rec.Body.Bytes(), &loginResp)
	if loginResp["token"] == "" {
		t.Fatalf("expected token in login response")
	}
}

func TestRoleSelect(t *testing.T) {
	router, handler := setupTestRouter(t)

	handler.roleLookup = func(ctx context.Context, roleID string) (*models.Role, error) {
		if roleID != "role-1" {
			return nil, errRoleNotFound
		}
		return &models.Role{
			ID:          "role-1",
			Name:        "Sherlock Holmes",
			Description: "Detective",
			CreatedAt:   time.Date(1892, time.January, 1, 0, 0, 0, 0, time.UTC),
		}, nil
	}

	selectBody := map[string]string{
		"roleId": "role-1",
	}

	rec := httptest.NewRecorder()
	req := newJSONRequest(t, http.MethodPost, "/api/role/select", selectBody)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var roleResp map[string]any
	decodeBody(t, rec.Body.Bytes(), &roleResp)
	if roleResp["id"] != "role-1" {
		t.Fatalf("expected role id role-1, got %v", roleResp["id"])
	}
}

func newJSONRequest(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("failed to marshal body: %v", err)
	}

	req, err := http.NewRequest(method, path, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return req
}

func decodeBody(t *testing.T, data []byte, out any) {
	t.Helper()
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
}
