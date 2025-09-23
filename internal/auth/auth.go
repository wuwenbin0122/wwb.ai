package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/wuwenbin0122/wwb.ai/internal/models"
)

var (
	ErrSecretRequired     = errors.New("auth: jwt secret required")
	ErrUserExists         = errors.New("auth: user already exists")
	ErrEmailExists        = errors.New("auth: email already registered")
	ErrUsernameRequired   = errors.New("auth: username is required")
	ErrPasswordTooWeak    = errors.New("auth: password must be at least 6 characters")
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	ErrInvalidToken       = errors.New("auth: invalid token")
)

type RegisterInput struct {
	Username string
	Email    string
	Password string
}

type LoginInput struct {
	Identifier string
	Password   string
}

type AuthResult struct {
	Token     string
	ExpiresAt time.Time
	User      models.User
}

type Service struct {
	secret []byte
	ttl    time.Duration

	mu           sync.RWMutex
	usersByName  map[string]*models.User
	usersByEmail map[string]*models.User
}

func NewService(secret string, ttl time.Duration) (*Service, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil, ErrSecretRequired
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}

	return &Service{
		secret:       []byte(secret),
		ttl:          ttl,
		usersByName:  make(map[string]*models.User),
		usersByEmail: make(map[string]*models.User),
	}, nil
}

func (s *Service) Register(ctx context.Context, input RegisterInput) (*AuthResult, error) {
	_ = ctx

	username := strings.TrimSpace(input.Username)
	if username == "" {
		return nil, ErrUsernameRequired
	}
	if len(strings.TrimSpace(input.Password)) < 6 {
		return nil, ErrPasswordTooWeak
	}

	emailKey := normalizeEmail(input.Email)
	usernameKey := strings.ToLower(username)

	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	user := &models.User{
		ID:           uuid.NewString(),
		Username:     username,
		Email:        strings.TrimSpace(input.Email),
		PasswordHash: string(hash),
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.usersByName[usernameKey]; exists {
		return nil, ErrUserExists
	}

	if emailKey != "" {
		if _, exists := s.usersByEmail[emailKey]; exists {
			return nil, ErrEmailExists
		}
	}

	s.usersByName[usernameKey] = user
	if emailKey != "" {
		s.usersByEmail[emailKey] = user
	}

	token, expiresAt, err := s.generateToken(user)
	if err != nil {
		return nil, err
	}

	return &AuthResult{
		Token:     token,
		ExpiresAt: expiresAt,
		User:      user.Sanitize(),
	}, nil
}

func (s *Service) Login(ctx context.Context, input LoginInput) (*AuthResult, error) {
	_ = ctx

	identifier := strings.TrimSpace(input.Identifier)
	if identifier == "" || strings.TrimSpace(input.Password) == "" {
		return nil, ErrInvalidCredentials
	}

	s.mu.RLock()
	user := s.lookupUserLocked(identifier)
	s.mu.RUnlock()

	if user == nil {
		return nil, ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(input.Password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	s.mu.Lock()
	user.UpdatedAt = time.Now().UTC()
	s.mu.Unlock()

	token, expiresAt, err := s.generateToken(user)
	if err != nil {
		return nil, err
	}

	return &AuthResult{
		Token:     token,
		ExpiresAt: expiresAt,
		User:      user.Sanitize(),
	}, nil
}

func (s *Service) VerifyToken(token string) (*jwt.RegisteredClaims, error) {
	parsed, err := jwt.ParseWithClaims(token, &jwt.RegisteredClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return s.secret, nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := parsed.Claims.(*jwt.RegisteredClaims)
	if !ok || !parsed.Valid {
		return nil, ErrInvalidToken
	}

	return claims, nil
}

func (s *Service) generateToken(user *models.User) (string, time.Time, error) {
	expiresAt := time.Now().UTC().Add(s.ttl)
	claims := jwt.RegisteredClaims{
		Subject:   user.ID,
		IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
		ExpiresAt: jwt.NewNumericDate(expiresAt),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.secret)
	if err != nil {
		return "", time.Time{}, err
	}

	return signed, expiresAt, nil
}

func (s *Service) lookupUserLocked(identifier string) *models.User {
	key := strings.ToLower(identifier)
	if user, ok := s.usersByName[key]; ok {
		return user
	}

	if user, ok := s.usersByEmail[normalizeEmail(identifier)]; ok {
		return user
	}

	return nil
}

func normalizeEmail(email string) string {
	return strings.TrimSpace(strings.ToLower(email))
}
