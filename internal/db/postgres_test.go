package db_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wuwenbin0122/wwb.ai/internal/db"
	"github.com/wuwenbin0122/wwb.ai/internal/utils"
)

func TestPostgresEnsureSchemaAndCRUD(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set; skipping postgres integration test")
	}

	cfg := utils.PostgresConfig{
		DSN:            dsn,
		ConnectTimeout: 5 * time.Second,
	}

	store, err := db.NewPostgres(context.Background(), cfg)
	if err != nil {
		t.Fatalf("failed to connect to postgres: %v", err)
	}
	defer store.Close()

	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("ensure schema failed: %v", err)
	}

	ctx := context.Background()

	userID := uuid.NewString()
	username := "user_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	insertUserSQL := fmt.Sprintf("INSERT INTO users (id, username, password, created_at) VALUES ('%s', '%s', '%s', NOW())", userID, username, "secret")
	if _, err := store.Pool.Exec(ctx, insertUserSQL); err != nil {
		t.Fatalf("failed to insert user: %v", err)
	}
	defer store.Pool.Exec(ctx, fmt.Sprintf("DELETE FROM users WHERE id = '%s'", userID))

	var fetched string
	queryUserSQL := fmt.Sprintf("SELECT username FROM users WHERE id = '%s'", userID)
	if err := store.Pool.QueryRow(ctx, queryUserSQL).Scan(&fetched); err != nil {
		t.Fatalf("failed to fetch user: %v", err)
	}
	if fetched != username {
		t.Fatalf("expected username %s, got %s", username, fetched)
	}

	roleID := uuid.NewString()
	roleName := "role_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	insertRoleSQL := fmt.Sprintf("INSERT INTO roles (id, name, description, created_at) VALUES ('%s', '%s', '%s', NOW())", roleID, roleName, "test role")
	if _, err := store.Pool.Exec(ctx, insertRoleSQL); err != nil {
		t.Fatalf("failed to insert role: %v", err)
	}
	defer store.Pool.Exec(ctx, fmt.Sprintf("DELETE FROM roles WHERE id = '%s'", roleID))

	convID := uuid.NewString()
	insertConversationSQL := fmt.Sprintf(
		"INSERT INTO conversations (id, user_id, role_id, content, timestamp) VALUES ('%s', '%s', '%s', '%s', NOW())",
		convID,
		userID,
		roleID,
		"hello",
	)
	if _, err := store.Pool.Exec(ctx, insertConversationSQL); err != nil {
		t.Fatalf("failed to insert conversation: %v", err)
	}
	defer store.Pool.Exec(ctx, fmt.Sprintf("DELETE FROM conversations WHERE id = '%s'", convID))

	var content string
	queryConversationSQL := fmt.Sprintf("SELECT content FROM conversations WHERE id = '%s'", convID)
	if err := store.Pool.QueryRow(ctx, queryConversationSQL).Scan(&content); err != nil {
		t.Fatalf("failed to fetch conversation: %v", err)
	}
	if content != "hello" {
		t.Fatalf("expected conversation content 'hello', got %s", content)
	}
}
