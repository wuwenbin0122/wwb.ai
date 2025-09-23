package utils

import (
	"strings"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	globalLogger *zap.Logger
	loggerOnce   sync.Once
)

func NewLogger(cfg LoggingConfig) (*zap.Logger, error) {
	level := zapcore.InfoLevel
	if err := level.Set(strings.ToLower(cfg.Level)); err != nil {
		level = zapcore.InfoLevel
	}

	encoding := strings.ToLower(cfg.Encoding)
	if encoding == "" {
		encoding = "console"
	}

	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderCfg.TimeKey = "time"
	encoderCfg.MessageKey = "msg"
	encoderCfg.EncodeLevel = zapcore.CapitalLevelEncoder

	if encoding == "console" {
		encoderCfg = zap.NewDevelopmentEncoderConfig()
		encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	}

	zapCfg := zap.Config{
		Level:             zap.NewAtomicLevelAt(level),
		Development:       cfg.Development,
		Encoding:          encoding,
		EncoderConfig:     encoderCfg,
		OutputPaths:       []string{"stdout"},
		ErrorOutputPaths:  []string{"stderr"},
		DisableCaller:     !cfg.EnableCaller,
		DisableStacktrace: !cfg.Development,
		InitialFields: map[string]interface{}{
			"service": cfg.ServiceName,
		},
	}

	logger, err := zapCfg.Build()
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(cfg.ServiceName) != "" {
		logger = logger.Named(cfg.ServiceName)
	}

	replaceGlobal(logger)

	return logger, nil
}

func MustNewLogger(cfg LoggingConfig) *zap.Logger {
	logger, err := NewLogger(cfg)
	if err != nil {
		panic(err)
	}
	return logger
}

func Logger() *zap.Logger {
	loggerOnce.Do(func() {
		if globalLogger == nil {
			logger, _ := zap.NewProduction()
			replaceGlobal(logger)
		}
	})
	return globalLogger
}

func replaceGlobal(logger *zap.Logger) {
	zap.ReplaceGlobals(logger)
	globalLogger = logger
}
