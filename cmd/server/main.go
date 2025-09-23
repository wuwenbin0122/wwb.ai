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

	ctx := context.Background()

	postgres, err := db.NewPostgres(ctx, cfg.Postgres)
	if err != nil {
		log.Fatalf("postgres: failed to connect: %v", err)
	}
	defer postgres.Close()

	if err := postgres.Ping(ctx); err != nil {
		log.Fatalf("postgres: ping failed: %v", err)
	}
	if err := postgres.EnsureSchema(ctx); err != nil {
		log.Fatalf("postgres: ensure schema: %v", err)
	}

	mongoStore, err := db.NewMongo(ctx, cfg.Mongo)
	if err != nil {
		log.Fatalf("mongo: failed to connect: %v", err)
	}
	defer func() {
		if err := mongoStore.Close(context.Background()); err != nil {
			log.Printf("mongo: close error: %v", err)
		}
	}()

	if err := mongoStore.EnsureCollections(ctx); err != nil {
		log.Fatalf("mongo: ensure collections: %v", err)
	}

	authService, err := auth.NewService(cfg.JWTSecret, 24*time.Hour)
	if err != nil {
		log.Fatalf("failed to initialise auth service: %v", err)
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
		log.Printf("server listening on %s", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server crashed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}

	log.Println("server stopped cleanly")
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
