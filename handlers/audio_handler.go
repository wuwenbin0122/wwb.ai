package handlers

import (
	"context"
	"encoding/base64"
	json "encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/wuwenbin0122/wwb.ai/config"
	"github.com/wuwenbin0122/wwb.ai/services"
	"go.uber.org/zap"
)

// AudioHandler coordinates ASR and TTS workflow endpoints.
type AudioHandler struct {
	cfg    *config.Config
	asr    *services.ASRService
	tts    *services.TTSService
	logger *zap.SugaredLogger
}

// NewAudioHandler builds a new AudioHandler.
func NewAudioHandler(cfg *config.Config, asr *services.ASRService, tts *services.TTSService, logger *zap.SugaredLogger) *AudioHandler {
	return &AudioHandler{cfg: cfg, asr: asr, tts: tts, logger: logger}
}

// asrRequest represents the incoming payload for ASR processing.
type asrRequest struct {
	Token       string   `json:"token"`
	AudioChunks []string `json:"audio_chunks"`
	TimeoutMS   int      `json:"timeout_ms"`
}

// ttsRequest represents the incoming payload for TTS synthesis.
type ttsRequest struct {
	Token      string  `json:"token"`
	Text       string  `json:"text"`
	VoiceType  string  `json:"voice_type"`
	Encoding   string  `json:"encoding"`
	SpeedRatio float64 `json:"speed_ratio"`
	TimeoutMS  int     `json:"timeout_ms"`
}

// HandleASR accepts base64 encoded audio chunks, forwards them to Qiniu ASR, and returns transcription results.
func (h *AudioHandler) HandleASR(c *gin.Context) {
	var req asrRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request payload"})
		return
	}

	token := strings.TrimSpace(req.Token)
	if token == "" {
		token = strings.TrimSpace(h.cfg.QiniuAPIKey)
	}
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "qiniu token is required"})
		return
	}

	baseCtx := c.Request.Context()
	ctx := baseCtx
	var cancel context.CancelFunc
	if req.TimeoutMS > 0 {
		ctx, cancel = context.WithTimeout(baseCtx, time.Duration(req.TimeoutMS)*time.Millisecond)
	} else {
		ctx, cancel = context.WithTimeout(baseCtx, 2*time.Minute)
	}
	defer cancel()

	audioStream := make(chan []byte)
	go func() {
		defer close(audioStream)
		for _, chunk := range req.AudioChunks {
			data := strings.TrimSpace(chunk)
			if data == "" {
				continue
			}
			decoded, err := base64.StdEncoding.DecodeString(data)
			if err != nil {
				h.logger.Warnf("decode audio chunk failed: %v", err)
				continue
			}
			audioStream <- decoded
		}
	}()

	results := make([]services.ASRResult, 0)
	var mu sync.Mutex

	err := h.asr.StreamRecognize(ctx, token, audioStream, func(res services.ASRResult) {
		mu.Lock()
		results = append(results, res)
		mu.Unlock()
	})

	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		h.logger.Warnf("asr stream failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "asr processing failed"})
		return
	}

	resp := make([]gin.H, 0, len(results))
	for _, r := range results {
		resp = append(resp, gin.H{
			"text":     r.Text,
			"is_final": r.IsFinal,
			"raw":      jsonRawToString(r.Raw),
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"results": resp,
	})
}

// HandleTTS accepts text and parameters, forwards them to Qiniu TTS, and returns audio chunks as base64 strings.
func (h *AudioHandler) HandleTTS(c *gin.Context) {
	var req ttsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request payload"})
		return
	}

	token := strings.TrimSpace(req.Token)
	if token == "" {
		token = strings.TrimSpace(h.cfg.QiniuAPIKey)
	}
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "qiniu token is required"})
		return
	}

	if strings.TrimSpace(req.Text) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "text is required"})
		return
	}

	baseCtx := c.Request.Context()
	ctx := baseCtx
	var cancel context.CancelFunc
	if req.TimeoutMS > 0 {
		ctx, cancel = context.WithTimeout(baseCtx, time.Duration(req.TimeoutMS)*time.Millisecond)
	} else {
		ctx, cancel = context.WithTimeout(baseCtx, 2*time.Minute)
	}
	defer cancel()

	chunks := make([]gin.H, 0)
	err := h.tts.Synthesize(ctx, token, services.TTSRequest{
		Text:       req.Text,
		VoiceType:  req.VoiceType,
		Encoding:   req.Encoding,
		SpeedRatio: req.SpeedRatio,
	}, func(chunk services.TTSChunk) {
		if len(chunk.Audio) == 0 {
			return
		}
		encoded := base64.StdEncoding.EncodeToString(chunk.Audio)
		chunks = append(chunks, gin.H{
			"audio":    encoded,
			"is_final": chunk.IsFinal,
			"raw":      jsonRawToString(chunk.Raw),
		})
	})

	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		h.logger.Warnf("tts synth failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "tts processing failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"chunks": chunks,
	})
}

func jsonRawToString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	return string(raw)
}

var audioWSUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// HandleAudioStream establishes a websocket session for streaming audio and issuing TTS requests.
func (h *AudioHandler) HandleAudioStream(c *gin.Context) {
	token := strings.TrimSpace(c.Query("token"))
	if token == "" {
		token = strings.TrimSpace(h.cfg.QiniuAPIKey)
	}
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "qiniu token is required"})
		return
	}

	conn, err := audioWSUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.logger.Warnf("upgrade audio websocket failed: %v", err)
		return
	}
	defer conn.Close()

	writer := &wsWriter{conn: conn}

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	audioStream := make(chan []byte, 32)
	var audioClosed int32
	closeAudio := func() {
		if atomic.CompareAndSwapInt32(&audioClosed, 0, 1) {
			close(audioStream)
		}
	}

	go func() {
		err := h.asr.StreamRecognize(ctx, token, audioStream, func(res services.ASRResult) {
			if strings.TrimSpace(res.Text) == "" {
				return
			}
			if writeErr := writer.WriteJSON(gin.H{
				"type":     "asr",
				"text":     res.Text,
				"is_final": res.IsFinal,
			}); writeErr != nil {
				h.logger.Warnf("write asr result failed: %v", writeErr)
			}
		})

		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			h.logger.Warnf("asr stream failed: %v", err)
			_ = writer.WriteJSON(gin.H{"type": "error", "scope": "asr", "message": "asr processing failed"})
		}

		_ = writer.WriteJSON(gin.H{"type": "asr_complete"})
		closeAudio()
		cancel()
	}()

	for {
		select {
		case <-ctx.Done():
			closeAudio()
			return
		default:
		}

		messageType, data, readErr := conn.ReadMessage()
		if readErr != nil {
			if !websocket.IsCloseError(readErr, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				h.logger.Warnf("audio websocket read error: %v", readErr)
			}
			closeAudio()
			return
		}

		if messageType == websocket.BinaryMessage {
			select {
			case audioStream <- data:
			case <-ctx.Done():
			}
			continue
		}

		var envelope map[string]interface{}
		if err := json.Unmarshal(data, &envelope); err != nil {
			_ = writer.WriteJSON(gin.H{"type": "error", "scope": "client", "message": "invalid message payload"})
			continue
		}

		msgType, _ := envelope["type"].(string)
		switch msgType {
		case "audio_complete":
			closeAudio()
		case "tts_request":
			text := strings.TrimSpace(getStringField(envelope, "text"))
			if text == "" {
				_ = writer.WriteJSON(gin.H{"type": "error", "scope": "tts", "message": "text is required"})
				continue
			}

			voice := strings.TrimSpace(getStringField(envelope, "voice_type"))
			encoding := strings.TrimSpace(getStringField(envelope, "encoding"))
			speed := getFloatField(envelope, "speed_ratio")

			go h.streamTTS(ctx, writer, token, services.TTSRequest{
				Text:       text,
				VoiceType:  voice,
				Encoding:   encoding,
				SpeedRatio: speed,
			})
		case "ping":
			_ = writer.WriteJSON(gin.H{"type": "pong"})
		default:
			_ = writer.WriteJSON(gin.H{"type": "error", "scope": "client", "message": "unknown message type"})
		}
	}
}

func (h *AudioHandler) streamTTS(ctx context.Context, writer *wsWriter, token string, req services.TTSRequest) {
	ttsCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	err := h.tts.Synthesize(ttsCtx, token, req, func(chunk services.TTSChunk) {
		if len(chunk.Audio) == 0 {
			return
		}

		encoded := base64.StdEncoding.EncodeToString(chunk.Audio)
		if writeErr := writer.WriteJSON(gin.H{
			"type":     "tts",
			"audio":    encoded,
			"is_final": chunk.IsFinal,
		}); writeErr != nil {
			h.logger.Warnf("write tts chunk failed: %v", writeErr)
		}
	})

	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		h.logger.Warnf("tts stream failed: %v", err)
		_ = writer.WriteJSON(gin.H{"type": "error", "scope": "tts", "message": "tts processing failed"})
		return
	}

	_ = writer.WriteJSON(gin.H{"type": "tts_complete"})
}

func getStringField(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		switch t := v.(type) {
		case string:
			return t
		case json.Number:
			return t.String()
		default:
			return strings.TrimSpace(fmt.Sprint(t))
		}
	}
	return ""
}

func getFloatField(m map[string]interface{}, key string) float64 {
	if v, ok := m[key]; ok {
		switch t := v.(type) {
		case float64:
			return t
		case json.Number:
			f, _ := t.Float64()
			return f
		case string:
			f, _ := strconv.ParseFloat(strings.TrimSpace(t), 64)
			return f
		}
	}
	return 0
}

type wsWriter struct {
	mu   sync.Mutex
	conn *websocket.Conn
}

func (w *wsWriter) WriteJSON(v interface{}) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteJSON(v)
}
