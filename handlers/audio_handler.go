package handlers

import (
	"context"
	"encoding/base64"
	json "encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
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

// NewAudioHandler builds a new AudioHandler.
func NewAudioHandler(cfg *config.Config, asr *services.ASRService, tts *services.TTSService, logger *zap.SugaredLogger) *AudioHandler {
	return &AudioHandler{cfg: cfg, asr: asr, tts: tts, logger: logger}
}

type asrRequest struct {
	Token       string   `json:"token"`
	AudioURL    string   `json:"audio_url"`
	AudioData   string   `json:"audio_data"`
	AudioChunks []string `json:"audio_chunks"`
	AudioFormat string   `json:"audio_format"`
	TimeoutMS   int      `json:"timeout_ms"`
}

type ttsRequest struct {
	Token      string  `json:"token"`
	Text       string  `json:"text"`
	VoiceType  string  `json:"voice_type"`
	Encoding   string  `json:"encoding"`
	SpeedRatio float64 `json:"speed_ratio"`
	TimeoutMS  int     `json:"timeout_ms"`
}

// HandleASR forwards a transcription request to Qiniu's REST API.
func (h *AudioHandler) HandleASR(c *gin.Context) {
	var req asrRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request payload", "detail": err.Error()})
		return
	}

	token := h.resolveToken(c, req.Token)
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "qiniu token is required"})
		return
	}

	input := services.ASRInput{Format: strings.TrimSpace(req.AudioFormat)}

	fallbackTimeout := 3 * time.Minute
	if trimmedURL := strings.TrimSpace(req.AudioURL); trimmedURL != "" {
		input.URL = trimmedURL
	} else {
		buffers, err := h.collectAudioBuffers(req)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid audio data", "detail": err.Error()})
			return
		}

		if len(buffers) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no audio data provided"})
			return
		}

		merged := mergeBuffers(buffers)
		if len(merged) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "audio payload was empty"})
			return
		}

		input.Data = merged
		if estimated := estimateAudioDurationMs(merged, input.Format); estimated > 0 {
			fallbackTimeout = computeASRTimeout(estimated)
		}
	}

	ctx, cancel := h.contextWithTimeout(c.Request.Context(), req.TimeoutMS, fallbackTimeout)
	defer cancel()

	result, err := h.asr.Recognize(ctx, token, input)
	if err != nil {
		h.logger.Warnf("asr recognize failed: %v", err)
		c.JSON(statusFromError(err), gin.H{"error": "asr processing failed", "detail": err.Error()})
		return
	}

	if result == nil {
		h.logger.Warn("asr recognize returned no result without error")
		c.JSON(http.StatusBadGateway, gin.H{"error": "asr processing failed", "detail": "empty response"})
		return
	}

	if len(result.Raw) > 0 {
		var envelope interface{}
		if err := json.Unmarshal(result.Raw, &envelope); err == nil {
			c.JSON(http.StatusOK, envelope)
			return
		}
		// fallback to legacy payload if raw cannot be parsed
	}

	response := gin.H{
		"reqid":       result.ReqID,
		"text":        result.Text,
		"duration_ms": result.DurationMS,
	}

	c.JSON(http.StatusOK, response)
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

func (h *AudioHandler) collectAudioBuffers(req asrRequest) ([][]byte, error) {
	buffers := make([][]byte, 0, len(req.AudioChunks)+1)

	if data := strings.TrimSpace(req.AudioData); data != "" {
		decoded, err := decodeAudioField(data)
		if err != nil {
			return nil, fmt.Errorf("decode audio_data: %w", err)
		}
		buffers = append(buffers, decoded)
	}

	for idx, chunk := range req.AudioChunks {
		trimmed := strings.TrimSpace(chunk)
		if trimmed == "" {
			continue
		}
		decoded, err := decodeAudioField(trimmed)
		if err != nil {
			return nil, fmt.Errorf("decode audio_chunks[%d]: %w", idx, err)
		}
		buffers = append(buffers, decoded)
	}

	return buffers, nil
}

func decodeAudioField(value string) ([]byte, error) {
	payload := value
	if strings.HasPrefix(payload, "data:") {
		parts := strings.SplitN(payload, ",", 2)
		if len(parts) != 2 {
			return nil, errors.New("malformed data URI")
		}
		payload = parts[1]
	}

	payload = strings.TrimSpace(payload)
	if payload == "" {
		return nil, errors.New("empty audio payload")
	}

	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, err
	}
	return decoded, nil
}

func mergeBuffers(chunks [][]byte) []byte {
	if len(chunks) == 0 {
		return nil
	}

	total := 0
	for _, chunk := range chunks {
		total += len(chunk)
	}

	merged := make([]byte, 0, total)
	for _, chunk := range chunks {
		if len(chunk) == 0 {
			continue
		}
		merged = append(merged, chunk...)
	}

	return merged
}

func estimateAudioDurationMs(data []byte, format string) int {
	if len(data) == 0 {
		return 0
	}

	format = strings.ToLower(strings.TrimSpace(format))
	compute := func(pcmBytes int) int {
		if pcmBytes <= 0 {
			return 0
		}
		samples := pcmBytes / 2
		if samples <= 0 {
			return 0
		}
		seconds := float64(samples) / 16000.0
		if seconds <= 0 {
			return 0
		}
		return int(math.Round(seconds * 1000))
	}

	switch {
	case format == "" || format == "wav" || format == "wave" || strings.HasSuffix(format, "/wav") || strings.HasSuffix(format, "/wave"):
		if len(data) <= 44 {
			return 0
		}
		return compute(len(data) - 44)
	case strings.Contains(format, "pcm"):
		return compute(len(data))
	default:
		return 0
	}
}

func computeASRTimeout(durationMs int) time.Duration {
	estimated := time.Duration(durationMs) * time.Millisecond
	if estimated <= 0 {
		return 3 * time.Minute
	}
	timeout := estimated*2 + 30*time.Second
	if timeout < 90*time.Second {
		timeout = 90 * time.Second
	}
	if timeout > 5*time.Minute {
		timeout = 5 * time.Minute
	}
	return timeout
}
