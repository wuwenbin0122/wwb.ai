package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/wuwenbin0122/wwb.ai/config"
	"github.com/wuwenbin0122/wwb.ai/services"
	"go.uber.org/zap"
)

// AudioHandler orchestrates the ASR/TTS HTTP endpoints exposed by the backend.
type AudioHandler struct {
	cfg    *config.Config
	asr    *services.ASRService
	tts    *services.TTSService
	logger *zap.SugaredLogger
}

var asrUpgrader = websocket.Upgrader{
	ReadBufferSize:  32 * 1024,
	WriteBufferSize: 32 * 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// NewAudioHandler builds a new AudioHandler.
func NewAudioHandler(cfg *config.Config, asr *services.ASRService, tts *services.TTSService, logger *zap.SugaredLogger) *AudioHandler {
	return &AudioHandler{cfg: cfg, asr: asr, tts: tts, logger: logger}
}

type asrClientMessage struct {
	Type       string `json:"type"`
	SampleRate int    `json:"sampleRate"`
	Channels   int    `json:"channels"`
	Bits       int    `json:"bits"`
	Token      string `json:"token"`
}

type ttsRequest struct {
	Token      string  `json:"token"`
	Text       string  `json:"text"`
	VoiceType  string  `json:"voice_type"`
	Encoding   string  `json:"encoding"`
	SpeedRatio float64 `json:"speed_ratio"`
	TimeoutMS  int     `json:"timeout_ms"`
}

// HandleASRWebsocket proxies streaming audio to Qiniu's ASR WebSocket endpoint.
func (h *AudioHandler) HandleASRWebsocket(c *gin.Context) {
	token := h.resolveTokenFromQuery(c)
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "qiniu token is required"})
		return
	}

	conn, err := asrUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.logger.Warnf("asr websocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	var (
		stream       *services.ASRStream
		streamMu     sync.Mutex
		writeMu      sync.Mutex
		upstreamOnce sync.Once
		upstreamDone = make(chan struct{})
	)

	sendJSON := func(payload interface{}) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteJSON(payload)
	}

	sendError := func(message string, detail error) {
		errMsg := gin.H{"type": "error", "error": message}
		if detail != nil {
			errMsg["detail"] = detail.Error()
			h.logger.Warnf("asr websocket error: %s: %v", message, detail)
		} else {
			h.logger.Warnf("asr websocket error: %s", message)
		}
		_ = sendJSON(errMsg)
	}

	closeUpstream := func() {
		streamMu.Lock()
		current := stream
		stream = nil
		streamMu.Unlock()
		if current != nil {
			_ = current.Close()
		}
		upstreamOnce.Do(func() { close(upstreamDone) })
	}

	go func() {
		<-ctx.Done()
		closeUpstream()
	}()

	handleUpstream := func(s *services.ASRStream) {
		go func() {
			defer closeUpstream()
			for {
				msgType, payload, err := s.Conn.ReadMessage()
				if err != nil {
					if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
						h.logger.Warnf("qiniu asr websocket closed unexpectedly: %v", err)
					}
					sendError("upstream connection closed", err)
					return
				}

				switch msgType {
				case websocket.BinaryMessage:
					envelope, raw, err := services.ParseASRWSMessage(payload)
					if err != nil {
						sendError("parse upstream payload", err)
						continue
					}
					text, isFinal, duration := services.ExtractTranscript(envelope)
					event := gin.H{"type": "transcript", "is_final": isFinal}
					if text != "" {
						event["text"] = text
					}
					if duration > 0 {
						event["duration_ms"] = duration
					}
					if len(raw) > 0 {
						event["raw"] = json.RawMessage(raw)
					}
					if err := sendJSON(event); err != nil {
						h.logger.Warnf("send transcript to client failed: %v", err)
						return
					}
				case websocket.TextMessage:
					// Forward text control frames as-is for debugging.
					msg := strings.TrimSpace(string(payload))
					if msg != "" {
						_ = sendJSON(gin.H{"type": "upstream", "payload": msg})
					}
				default:
					// ignore
				}
			}
		}()
	}

	for {
		msgType, payload, err := conn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				h.logger.Warnf("client asr websocket closed: %v", err)
			}
			break
		}

		switch msgType {
		case websocket.TextMessage:
			var msg asrClientMessage
			if err := json.Unmarshal(payload, &msg); err != nil {
				sendError("invalid control message", err)
				continue
			}

			msgTypeLower := strings.ToLower(strings.TrimSpace(msg.Type))
			switch msgTypeLower {
			case "start":
				streamMu.Lock()
				alreadyStarted := stream != nil
				streamMu.Unlock()
				if alreadyStarted {
					sendError("asr stream already started", nil)
					continue
				}

				sessionToken := token
				if candidate := strings.TrimSpace(msg.Token); candidate != "" {
					sessionToken = candidate
				}

				sr := msg.SampleRate
				if sr <= 0 {
					sr = 16000
				}
				ch := msg.Channels
				if ch <= 0 {
					ch = 1
				}
				bits := msg.Bits
				if bits <= 0 {
					bits = 16
				}

				upstream, err := h.asr.OpenStream(ctx, sessionToken, sr, ch, bits)
				if err != nil {
					sendError("open upstream stream", err)
					continue
				}

				streamMu.Lock()
				stream = upstream
				streamMu.Unlock()

				handleUpstream(upstream)

				ack := gin.H{
					"type":       "ready",
					"sampleRate": sr,
					"channels":   ch,
					"bits":       bits,
				}
				if err := sendJSON(ack); err != nil {
					h.logger.Warnf("send ready event failed: %v", err)
					closeUpstream()
					return
				}

			case "stop":
				streamMu.Lock()
				current := stream
				streamMu.Unlock()
				if current != nil {
					if err := current.Writer.SendStop(); err != nil {
						sendError("send stop", err)
					}
				}

			case "ping":
				_ = sendJSON(gin.H{"type": "pong"})

			default:
			sendError("unsupported control message", fmt.Errorf("%s", msg.Type))
			}

		case websocket.BinaryMessage:
			streamMu.Lock()
			current := stream
			streamMu.Unlock()
			if current == nil {
				sendError("stream not initialized", errors.New("start message required before audio"))
				continue
			}
			if err := current.Writer.SendAudioChunk(payload); err != nil {
				sendError("forward audio chunk", err)
				closeUpstream()
				return
			}

		case websocket.CloseMessage:
			closeUpstream()
			return

		default:
			// ignore
		}
	}

	closeUpstream()
	<-upstreamDone
}

// HandleTTS forwards text-to-speech requests to Qiniu and returns the synthesized audio.
func (h *AudioHandler) HandleTTS(c *gin.Context) {
	var req ttsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request payload", "detail": err.Error()})
		return
	}

	token := h.resolveToken(c, req.Token)
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "qiniu token is required"})
		return
	}

	if strings.TrimSpace(req.Text) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "text is required"})
		return
	}

	ctx, cancel := h.contextWithTimeout(c.Request.Context(), req.TimeoutMS, 90*time.Second)
	defer cancel()

	result, err := h.tts.Synthesize(ctx, token, services.TTSRequest{
		Text:       req.Text,
		VoiceType:  req.VoiceType,
		Encoding:   req.Encoding,
		SpeedRatio: req.SpeedRatio,
	})
	if err != nil {
		h.logger.Warnf("tts synth failed: %v", err)
		c.JSON(statusFromError(err), gin.H{"error": "tts processing failed", "detail": err.Error()})
		return
	}

	encoded := base64.StdEncoding.EncodeToString(result.Audio)
	response := gin.H{
		"reqid":    result.ReqID,
		"audio":    encoded,
		"duration": result.Duration,
		"raw":      result.Raw,
	}

	c.JSON(http.StatusOK, response)
}

// HandleVoiceList proxies the GET /voice/list endpoint.
func (h *AudioHandler) HandleVoiceList(c *gin.Context) {
	token := h.resolveTokenFromQuery(c)
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "qiniu token is required"})
		return
	}

	timeoutMS := 0
	if raw := strings.TrimSpace(c.Query("timeout_ms")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			timeoutMS = parsed
		}
	}

	ctx, cancel := h.contextWithTimeout(c.Request.Context(), timeoutMS, 30*time.Second)
	defer cancel()

	voices, err := h.tts.ListVoices(ctx, token)
	if err != nil {
		h.logger.Warnf("list voices failed: %v", err)
		c.JSON(statusFromError(err), gin.H{"error": "voice list failed", "detail": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"voices": voices})
}

func (h *AudioHandler) resolveToken(c *gin.Context, explicit string) string {
	if token := strings.TrimSpace(explicit); token != "" {
		return token
	}

	if header := parseAuthorizationToken(c.GetHeader("Authorization")); header != "" {
		return header
	}

	return strings.TrimSpace(h.cfg.QiniuAPIKey)
}

func (h *AudioHandler) resolveTokenFromQuery(c *gin.Context) string {
	if token := strings.TrimSpace(c.Query("token")); token != "" {
		return token
	}

	if header := parseAuthorizationToken(c.GetHeader("Authorization")); header != "" {
		return header
	}

	return strings.TrimSpace(h.cfg.QiniuAPIKey)
}

func (h *AudioHandler) contextWithTimeout(parent context.Context, timeoutMS int, fallback time.Duration) (context.Context, context.CancelFunc) {
	if timeoutMS > 0 {
		return context.WithTimeout(parent, time.Duration(timeoutMS)*time.Millisecond)
	}
	if fallback <= 0 {
		fallback = 30 * time.Second
	}
	return context.WithTimeout(parent, fallback)
}

func statusFromError(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return http.StatusGatewayTimeout
	}
	return http.StatusBadGateway
}

func parseAuthorizationToken(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}

	if strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return strings.TrimSpace(header[7:])
	}

	return ""
}

// legacy helpers removed: streaming ASR no longer accepts REST payloads
