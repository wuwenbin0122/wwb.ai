package utils

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ServerPort string
	JWTSecret  string
	Postgres   PostgresConfig
	Mongo      MongoConfig
	Logging    LoggingConfig
	QiniuAI    QiniuAIConfig
}

type PostgresConfig struct {
	DSN               string
	Host              string
	Port              int
	User              string
	Password          string
	Database          string
	MaxConns          int32
	MinConns          int32
	MaxConnLifetime   time.Duration
	MaxConnIdleTime   time.Duration
	HealthCheckPeriod time.Duration
	ConnectTimeout    time.Duration
}

type MongoConfig struct {
	URI            string
	Database       string
	ConnectTimeout time.Duration
}

type LoggingConfig struct {
	Level        string
	Encoding     string
	Development  bool
	EnableCaller bool
	ServiceName  string
}

type QiniuAIConfig struct {
	PrimaryEndpoint string
	BackupEndpoint  string
	ActiveEndpoint  string
	APIKey          string
}

func (q QiniuAIConfig) BaseURL() string {
	if strings.TrimSpace(q.ActiveEndpoint) != "" {
		return q.ActiveEndpoint
	}
	return q.PrimaryEndpoint
}

func LoadConfig() (*Config, error) {
	port := envOrDefault("PORT", "8080")
	jwtSecret := envOrDefault("JWT_SECRET", "dev-secret")

	pgPort, _ := strconv.Atoi(envOrDefault("POSTGRES_PORT", "5433"))
	maxConns := parseInt32(envOrDefault("POSTGRES_MAX_CONNS", "8"), 8)
	minConns := parseInt32(envOrDefault("POSTGRES_MIN_CONNS", "1"), 1)

	logging := LoggingConfig{
		Level:        strings.ToLower(envOrDefault("LOG_LEVEL", "info")),
		Encoding:     strings.ToLower(envOrDefault("LOG_ENCODING", "console")),
		Development:  parseBool(envOrDefault("LOG_DEVELOPMENT", "false"), false),
		EnableCaller: parseBool(envOrDefault("LOG_CALLER", "false"), false),
		ServiceName:  envOrDefault("SERVICE_NAME", "wwb-ai-server"),
	}

	primaryEndpoint := envOrDefault("QINIU_PRIMARY_ENDPOINT", "https://openai.qiniu.com/v1")
	backupEndpoint := envOrDefault("QINIU_BACKUP_ENDPOINT", "https://api.qnaigc.com/v1")

	cfg := &Config{
		ServerPort: port,
		JWTSecret:  jwtSecret,
		Postgres: PostgresConfig{
			DSN:               os.Getenv("POSTGRES_DSN"),
			Host:              envOrDefault("POSTGRES_HOST", "localhost"),
			Port:              pgPort,
			User:              envOrDefault("POSTGRES_USER", "postgres"),
			Password:          envOrDefault("POSTGRES_PASSWORD", "wwb666666"),
			Database:          envOrDefault("POSTGRES_DB", "postgres"),
			MaxConns:          maxConns,
			MinConns:          minConns,
			MaxConnLifetime:   parseDuration(envOrDefault("POSTGRES_MAX_CONN_LIFETIME", "1h"), time.Hour),
			MaxConnIdleTime:   parseDuration(envOrDefault("POSTGRES_MAX_CONN_IDLE", "30m"), 30*time.Minute),
			HealthCheckPeriod: parseDuration(envOrDefault("POSTGRES_HEALTH_CHECK_PERIOD", "1m"), time.Minute),
			ConnectTimeout:    parseDuration(envOrDefault("POSTGRES_CONNECT_TIMEOUT", "5s"), 5*time.Second),
		},
		Mongo: MongoConfig{
			URI:            envOrDefault("MONGO_URI", "mongodb://localhost:27017"),
			Database:       envOrDefault("MONGO_DATABASE", "local"),
			ConnectTimeout: parseDuration(envOrDefault("MONGO_CONNECT_TIMEOUT", "5s"), 5*time.Second),
		},
		Logging: logging,
		QiniuAI: QiniuAIConfig{
			PrimaryEndpoint: primaryEndpoint,
			BackupEndpoint:  backupEndpoint,
			ActiveEndpoint:  envOrDefault("QINIU_API_ENDPOINT", primaryEndpoint),
			APIKey:          os.Getenv("QINIU_API_KEY"),
		},
	}

	return cfg, nil
}

func (c PostgresConfig) BuildDSN() string {
	if c.DSN != "" {
		return c.DSN
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s", c.User, c.Password, c.Host, c.Port, c.Database)
}

func envOrDefault(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func parseDuration(value string, fallback time.Duration) time.Duration {
	d, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return d
}

func parseInt32(value string, fallback int32) int32 {
	i, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return int32(i)
}

func parseBool(value string, fallback bool) bool {
	v, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return fallback
	}
	return v
}
