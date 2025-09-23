package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

func NewMongoClient(ctx context.Context, uri string) (*mongo.Client, error) {
	if uri == "" {
		return nil, errors.New("mongo connection uri is empty")
	}

	opts := options.Client().ApplyURI(uri)

	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(dialCtx, opts)
	if err != nil {
		return nil, fmt.Errorf("connect to mongo: %w", err)
	}

	pingCtx, pingCancel := context.WithTimeout(ctx, 3*time.Second)
	defer pingCancel()

	if err := client.Ping(pingCtx, readpref.Primary()); err != nil {
		_ = client.Disconnect(pingCtx)
		return nil, fmt.Errorf("ping mongo: %w", err)
	}

	return client, nil
}
