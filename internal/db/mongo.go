package db

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/wuwenbin0122/wwb.ai/internal/utils"
)

type Mongo struct {
	Client        *mongo.Client
	Database      *mongo.Database
	Roles         *mongo.Collection
	Conversations *mongo.Collection
}

func NewMongo(ctx context.Context, cfg utils.MongoConfig) (*Mongo, error) {
	if cfg.URI == "" {
		return nil, fmt.Errorf("mongo: uri is required")
	}

	clientOpts := options.Client().ApplyURI(cfg.URI)
	if cfg.ConnectTimeout > 0 {
		clientOpts.SetServerSelectionTimeout(cfg.ConnectTimeout)
	}

	ctx, cancel := context.WithTimeout(ctx, timeoutOrDefault(cfg.ConnectTimeout))
	defer cancel()

	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		return nil, fmt.Errorf("mongo: connect: %w", err)
	}

	db := client.Database(cfg.Database)
	store := &Mongo{
		Client:        client,
		Database:      db,
		Roles:         db.Collection("roles"),
		Conversations: db.Collection("conversations"),
	}

	return store, nil
}

func (m *Mongo) Close(ctx context.Context) error {
	if m == nil || m.Client == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return m.Client.Disconnect(ctx)
}

func (m *Mongo) EnsureCollections(ctx context.Context) error {
	if m == nil || m.Database == nil {
		return fmt.Errorf("mongo: database not initialised")
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, err := m.Roles.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "name", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return fmt.Errorf("mongo: ensure role index: %w", err)
	}

	_, err = m.Conversations.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "role_id", Value: 1}, {Key: "timestamp", Value: -1}},
	})
	if err != nil {
		return fmt.Errorf("mongo: ensure conversation index: %w", err)
	}

	return nil
}

func timeoutOrDefault(value time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return 10 * time.Second
}
