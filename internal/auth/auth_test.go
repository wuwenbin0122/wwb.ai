package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wuwenbin0122/wwb.ai/internal/auth"
)

func TestAuthServiceRegisterAndLogin(t *testing.T) {
	svc, err := auth.NewService("test-secret", time.Hour)
	if err != nil {
		t.Fatalf("unexpected error creating auth service: %v", err)
	}

	registerResult, err := svc.Register(context.Background(), auth.RegisterInput{
		Username: "alice",
		Email:    "alice@example.com",
		Password: "s3cret!",
	})
	if err != nil {
		t.Fatalf("register returned error: %v", err)
	}

	if registerResult.Token == "" {
		t.Fatalf("expected token on registration")
	}

	if registerResult.User.Username != "alice" {
		t.Fatalf("expected username alice, got %s", registerResult.User.Username)
	}

	if registerResult.User.Email != "alice@example.com" {
		t.Fatalf("expected email preserved")
	}

	if registerResult.User.ID == "" {
		t.Fatalf("expected user id to be populated")
	}

	claims, err := svc.VerifyToken(registerResult.Token)
	if err != nil {
		t.Fatalf("verify token failed: %v", err)
	}

	if claims.Subject != registerResult.User.ID {
		t.Fatalf("expected token subject %s, got %s", registerResult.User.ID, claims.Subject)
	}

	if _, err := svc.Register(context.Background(), auth.RegisterInput{
		Username: "alice",
		Email:    "other@example.com",
		Password: "another!",
	}); !errors.Is(err, auth.ErrUserExists) {
		t.Fatalf("expected duplicate username error, got %v", err)
	}

	loginResult, err := svc.Login(context.Background(), auth.LoginInput{
		Identifier: "alice",
		Password:   "s3cret!",
	})
	if err != nil {
		t.Fatalf("login returned error: %v", err)
	}

	if loginResult.Token == "" {
		t.Fatalf("expected token on login")
	}

	if loginResult.User.Username != "alice" {
		t.Fatalf("expected login user to be alice, got %s", loginResult.User.Username)
	}

	if _, err := svc.Login(context.Background(), auth.LoginInput{
		Identifier: "alice",
		Password:   "wrong",
	}); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("expected invalid credentials error, got %v", err)
	}
}
