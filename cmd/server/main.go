package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/wuwenbin0122/wwb.ai/config"
	"github.com/wuwenbin0122/wwb.ai/db"
	"go.uber.org/zap"
)

type webSocketHandler struct {
	upgrader websocket.Upgrader
	logger   *zap.SugaredLogger
}

func newWebSocketHandler(logger *zap.SugaredLogger) *webSocketHandler {
	return &webSocketHandler{
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
		logger: logger,
	}
}

func (h *webSocketHandler) handle(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Errorf("upgrade websocket: %v", err)
		return
	}
	defer conn.Close()

	h.logger.Infof("websocket client connected: %s", r.RemoteAddr)

	for {
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				h.logger.Warnf("read websocket message: %v", err)
			}
			break
		}

		if err := conn.WriteMessage(messageType, payload); err != nil {
			h.logger.Warnf("write websocket message: %v", err)
			break
		}
	}

	h.logger.Infof("websocket client disconnected: %s", r.RemoteAddr)
}

func main() {
	logger, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	defer logger.Sync() // ignore error caused by stdout/stderr being closed

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

	router.Use(func(c *gin.Context) {
		c.Set("postgres", pgPool)
		c.Set("mongo", mongoClient)
		c.Set("redis", redisClient)
		c.Next()
	})

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	wsHandler := newWebSocketHandler(sugar)
	router.GET("/ws", func(c *gin.Context) {
		wsHandler.handle(c.Writer, c.Request)
	})

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
