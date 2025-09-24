package handlers

import (
	"context"
	"encoding/base64"
	json "encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
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
