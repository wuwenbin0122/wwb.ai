package db_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"

	"github.com/wuwenbin0122/wwb.ai/internal/db"
	"github.com/wuwenbin0122/wwb.ai/internal/utils"
)

func TestMongoEnsureCollectionsAndCRUD(t *testing.T) {
	uri := os.Getenv("TEST_MONGO_URI")
	if uri == "" {
		t.Skip("TEST_MONGO_URI not set; skipping mongo integration test")
	}

	database := "wwb_ai_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")

	cfg := utils.MongoConfig{
		URI:            uri,
		Database:       database,
		ConnectTimeout: 5 * time.Second,
	}

	store, err := db.NewMongo(context.Background(), cfg)
	if err != nil {
		t.Fatalf("failed to connect to mongo: %v", err)
	}
	defer func() {
		ctx := context.Background()
		store.Database.Drop(ctx)
		store.Close(ctx)
	}()

	if err := store.EnsureCollections(context.Background()); err != nil {
		t.Fatalf("ensure collections failed: %v", err)
	}

	ctx := context.Background()

	roleID := uuid.NewString()
	_, err = store.Roles.InsertOne(ctx, bson.M{
		"_id":         roleID,
		"name":        "tester",
		"description": "integration test role",
		"created_at":  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("failed to insert role: %v", err)
	}

	convID := uuid.NewString()
	_, err = store.Conversations.InsertOne(ctx, bson.M{
		"_id":       convID,
		"user_id":   uuid.NewString(),
		"role_id":   roleID,
		"content":   "hello",
		"timestamp": time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("failed to insert conversation: %v", err)
	}

	var result bson.M
	if err := store.Conversations.FindOne(ctx, bson.M{"_id": convID}).Decode(&result); err != nil {
		t.Fatalf("failed to fetch conversation: %v", err)
	}

	if result["content"] != "hello" {
		t.Fatalf("expected conversation content 'hello', got %v", result["content"])
	}
}
