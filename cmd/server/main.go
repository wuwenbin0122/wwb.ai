package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"github.com/wuwenbin0122/wwb.ai/internal/api"
	"github.com/wuwenbin0122/wwb.ai/internal/auth"
	"github.com/wuwenbin0122/wwb.ai/internal/db"
	"github.com/wuwenbin0122/wwb.ai/internal/utils"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Printf("config: no .env file loaded: %v", err)
	}

	cfg, err := utils.LoadConfig()
	if err != nil {
		log.Fatalf("config: failed to load: %v", err)
	}

	logger, err := utils.NewLogger(cfg.Logging)
	if err != nil {
		log.Fatalf("logger: failed to initialise: %v", err)
	}
	defer func() {
		_ = logger.Sync()
	}()

	sugar := logger.Sugar()
	sugar.Infow("configuration loaded",
		"port", cfg.ServerPort,
		"postgres_host", cfg.Postgres.Host,
		"mongo_database", cfg.Mongo.Database,
		"qiniu_endpoint", cfg.QiniuAI.BaseURL(),
	)

	ctx := context.Background()

	postgres, err := db.NewPostgres(ctx, cfg.Postgres)
	if err != nil {
		sugar.Fatalw("postgres connection failed", "error", err)
	}
	defer postgres.Close()

	if err := postgres.Ping(ctx); err != nil {
		sugar.Fatalw("postgres ping failed", "error", err)
	}
	if err := postgres.EnsureSchema(ctx); err != nil {
		sugar.Fatalw("postgres schema ensure failed", "error", err)
	}

	mongoStore, err := db.NewMongo(ctx, cfg.Mongo)
	if err != nil {
		sugar.Fatalw("mongo connection failed", "error", err)
	}
	defer func() {
		if err := mongoStore.Close(context.Background()); err != nil {
			sugar.Warnw("mongo close error", "error", err)
		}
	}()

	if err := mongoStore.EnsureCollections(ctx); err != nil {
		sugar.Fatalw("mongo ensure collections failed", "error", err)
	}

	authService, err := auth.NewService(cfg.JWTSecret, 24*time.Hour)
	if err != nil {
		sugar.Fatalw("auth service initialisation failed", "error", err)
	}

	router := setupRouter(authService, postgres, mongoStore)

	server := &http.Server{
		Addr:         ":" + cfg.ServerPort,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		sugar.Infow("server listening", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			sugar.Fatalw("server crashed", "error", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		sugar.Warnw("graceful shutdown failed", "error", err)
	}

	sugar.Info("server stopped cleanly")
}

func setupRouter(authService *auth.Service, postgres *db.Postgres, mongoStore *db.Mongo) *gin.Engine {
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":    "ok",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
	})

	api.NewHandler(authService, postgres, mongoStore).RegisterRoutes(router)

	return router
}
