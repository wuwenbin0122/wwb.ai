package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"sync"

	"github.com/joho/godotenv"
)

type Config struct {
	ServerAddr        string
	DBURL             string
	MongoURI          string
	RedisURL          string
	QiniuAPIEndpoint  string
	QiniuAPIKey       string
	QiniuTTSEndpoint  string
	QiniuTTSVoiceType string
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

		cfg = &Config{
			ServerAddr:        getEnv("SERVER_ADDR", ":8080"),
			DBURL:             strings.TrimSpace(os.Getenv("DB_URL")),
			MongoURI:          strings.TrimSpace(os.Getenv("MONGO_URI")),
			RedisURL:          strings.TrimSpace(os.Getenv("REDIS_URL")),
			QiniuAPIEndpoint:  strings.TrimSpace(os.Getenv("QINIU_API_ENDPOINT")),
			QiniuAPIKey:       strings.TrimSpace(os.Getenv("QINIU_API_KEY")),
			QiniuTTSEndpoint:  strings.TrimSpace(os.Getenv("QINIU_TTS_ENDPOINT")),
			QiniuTTSVoiceType: strings.TrimSpace(os.Getenv("QINIU_TTS_VOICE_TYPE")),
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

	return fallback
}
