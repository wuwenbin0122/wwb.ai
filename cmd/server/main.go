package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/wuwenbin0122/wwb.ai/config"
	"github.com/wuwenbin0122/wwb.ai/db"
	"github.com/wuwenbin0122/wwb.ai/handlers"
	"github.com/wuwenbin0122/wwb.ai/services"
	"go.uber.org/zap"
)

func main() {
	logger, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	defer logger.Sync()

	sugar := logger.Sugar()

	cfg, err := config.Load()
	if err != nil {
		sugar.Fatalf("load configuration: %v", err)
	}

	baseCtx := context.Background()

	pgPool, err := db.NewPostgresPool(baseCtx, cfg.DBURL)
	if err != nil {
		sugar.Fatalf("connect postgres: %v", err)
	}
	defer pgPool.Close()

	mongoClient, err := db.NewMongoClient(baseCtx, cfg.MongoURI)
	if err != nil {
		sugar.Fatalf("connect mongo: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := mongoClient.Disconnect(shutdownCtx); err != nil {
			sugar.Warnf("disconnect mongo: %v", err)
		}
	}()

	redisClient, err := db.NewRedisClient(baseCtx, cfg.RedisURL)
	if err != nil {
		sugar.Fatalf("connect redis: %v", err)
	}
	defer func() {
		if err := redisClient.Close(); err != nil {
			sugar.Warnf("close redis: %v", err)
		}
	}()

	router := gin.Default()

	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	router.Use(func(c *gin.Context) {
		c.Set("postgres", pgPool)
		c.Set("mongo", mongoClient)
		c.Set("redis", redisClient)
		c.Next()
	})

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	roleHandler := handlers.NewRoleHandler(pgPool)
	router.GET("/api/roles", roleHandler.GetRoles)

	asrService := services.NewASRService(cfg, sugar)
	ttsService := services.NewTTSService(cfg, sugar)
	audioHandler := handlers.NewAudioHandler(cfg, asrService, ttsService, sugar)
	router.POST("/api/audio/asr", audioHandler.HandleASR)
	router.POST("/api/audio/tts", audioHandler.HandleTTS)
	router.GET("/api/audio/voices", audioHandler.HandleVoiceList)

	server := &http.Server{
		Addr:    cfg.ServerAddr,
		Handler: router,
	}

	go func() {
		sugar.Infof("backend server listening on %s", cfg.ServerAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			sugar.Fatalf("start server: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	sugar.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		sugar.Errorf("server shutdown: %v", err)
	}

	sugar.Info("server exited cleanly")
}
