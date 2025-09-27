package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/joho/godotenv"
)

type Config struct {
	ServerAddr        string
	DBURL             string
	MongoURI          string
	RedisURL          string
	QiniuAPIBaseURL   string
	QiniuAPIKey       string
	QiniuTTSVoiceType string
	QiniuTTSFormat    string
	QiniuASRModel     string
	ASRSampleRate     int
}

var (
	cfg     *Config
	loadErr error
	once    sync.Once
)

func Load() (*Config, error) {
	once.Do(func() {
		if err := loadEnvFiles(); err != nil {
			loadErr = fmt.Errorf("load env files: %w", err)
			return
		}

		apiBase := strings.TrimSpace(os.Getenv("QINIU_API_BASE"))
		if apiBase == "" {
			apiBase = strings.TrimSpace(os.Getenv("QINIU_API_BASE_URL"))
		}
		if apiBase == "" {
			apiBase = strings.TrimSpace(os.Getenv("QINIU_API_ENDPOINT"))
		}
		if apiBase == "" {
			apiBase = "https://openai.qiniu.com/v1"
		}

		sampleRate := parsePositiveInt(getEnv("ASR_SAMPLE_RATE", "16000"), 16000)

		cfg = &Config{
			ServerAddr:        getEnv("SERVER_ADDR", ":8080"),
			DBURL:             strings.TrimSpace(os.Getenv("DB_URL")),
			MongoURI:          strings.TrimSpace(os.Getenv("MONGO_URI")),
			RedisURL:          strings.TrimSpace(os.Getenv("REDIS_URL")),
			QiniuAPIBaseURL:   strings.TrimRight(apiBase, "/"),
			QiniuAPIKey:       strings.TrimSpace(os.Getenv("QINIU_API_KEY")),
			QiniuTTSVoiceType: strings.TrimSpace(os.Getenv("QINIU_TTS_VOICE_TYPE")),
			QiniuTTSFormat:    getEnv("QINIU_TTS_FORMAT", "mp3"),
			QiniuASRModel:     getEnv("QINIU_ASR_MODEL", "asr"),
			ASRSampleRate:     sampleRate,
		}

		loadErr = cfg.validate()
	})

	return cfg, loadErr
}

func loadEnvFiles() error {
	if err := godotenv.Load("config/.env"); err != nil {
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) {
			// ignore missing config/.env so that environment variables can be supplied externally
			return nil
		}

		return err
	}

	return nil
}

func (c *Config) validate() error {
	missing := make([]string, 0, 3)

	if c.DBURL == "" {
		missing = append(missing, "DB_URL")
	}

	if c.MongoURI == "" {
		missing = append(missing, "MONGO_URI")
	}

	if c.RedisURL == "" {
		missing = append(missing, "REDIS_URL")
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}

	return nil
}

func getEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}

	return strings.TrimSpace(fallback)
}

func parsePositiveInt(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}

	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return fallback
	}

	return value
}
