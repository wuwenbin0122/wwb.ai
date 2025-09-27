package handlers

import (
	"context"
	"encoding/base64"
	json "encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
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

type asrStreamStartMessage struct {
	Token       string `json:"token"`
	AudioFormat string `json:"audio_format"`
	SampleRate  int    `json:"sample_rate"`
	Channels    int    `json:"channels"`
	Bits        int    `json:"bits"`
	TimeoutMS   int    `json:"timeout_ms"`
}

var asrStreamUpgrader = websocket.Upgrader{
	ReadBufferSize:  8 * 1024,
	WriteBufferSize: 8 * 1024,
	CheckOrigin: func(*http.Request) bool {
		return true
	},
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
		if estimated := estimateAudioDurationMs(merged, input.Format, h.cfg.ASRSampleRate); estimated > 0 {
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

// HandleASRStream upgrades clients to a websocket and proxies audio frames to Qiniu's streaming ASR API.
func (h *AudioHandler) HandleASRStream(c *gin.Context) {
	conn, err := asrStreamUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.logger.Warnf("upgrade asr stream failed: %v", err)
		return
	}
	defer conn.Close()

	conn.SetReadLimit(8 * 1024 * 1024)

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	var writeMu sync.Mutex
	sendJSON := func(payload interface{}) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteJSON(payload)
	}
	sendError := func(message string) {
		_ = sendJSON(map[string]interface{}{"type": "error", "error": message})
	}

	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	messageType, payload, err := conn.ReadMessage()
	if err != nil {
		h.logger.Debugf("read asr stream start failed: %v", err)
		return
	}

	if messageType != websocket.TextMessage {
		sendError("expected text frame to initialize stream")
		return
	}

	var start asrStreamStartMessage
	if err := json.Unmarshal(payload, &start); err != nil {
		sendError(fmt.Sprintf("invalid start message: %v", err))
		return
	}

	format := strings.ToLower(strings.TrimSpace(start.AudioFormat))
	if format != "" && !strings.Contains(format, "pcm") {
		sendError("streaming ASR only supports PCM audio")
		return
	}

	token := h.resolveToken(c, start.Token)
	if token == "" {
		sendError("qiniu token is required")
		return
	}

	cfg := services.ASRStreamConfig{
		SampleRate: start.SampleRate,
		Channels:   start.Channels,
		Bits:       start.Bits,
	}

	session, err := h.asr.OpenStream(ctx, token, cfg)
	if err != nil {
		h.logger.Warnf("open asr stream failed: %v", err)
		sendError(err.Error())
		return
	}
	defer session.Close()

	_ = conn.SetReadDeadline(time.Time{})

	if err := sendJSON(map[string]interface{}{"type": "ready"}); err != nil {
		h.logger.Debugf("send ready message failed: %v", err)
	}

	var stopOnce sync.Once
	stopSession := func() {
		stopOnce.Do(func() {
			if err := session.SendStop(); err != nil {
				h.logger.Debugf("send asr stop failed: %v", err)
			}
		})
	}

	results := make(chan error, 1)
	go func() {
		defer close(results)
		for {
			if ctx.Err() != nil {
				results <- ctx.Err()
				return
			}

			readCtx, cancelRead := h.contextWithTimeout(ctx, start.TimeoutMS, 2*time.Minute)
			event, readErr := session.NextEvent(readCtx)
			cancelRead()

			if readErr != nil {
				results <- readErr
				return
			}

			if event == nil {
				continue
			}

			if event.Err != nil {
				results <- event.Err
				return
			}

			if event.Text == "" && len(event.Raw) == 0 && !event.IsFinal {
				continue
			}

			response := map[string]interface{}{"type": "partial"}
			if event.IsFinal {
				response["type"] = "final"
			}
			if event.Text != "" {
				response["text"] = event.Text
			}
			if event.ReqID != "" {
				response["reqid"] = event.ReqID
			}
			if event.DurationMS > 0 {
				response["duration_ms"] = event.DurationMS
			}
			if len(event.Raw) > 0 {
				response["raw"] = event.Raw
			}

			if err := sendJSON(response); err != nil {
				results <- err
				return
			}

			if event.IsFinal {
				results <- nil
				return
			}
		}
	}()

	finished := false
	for !finished {
		messageType, payload, err = conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) || errors.Is(err, context.Canceled) {
				break
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			if errors.Is(err, io.EOF) {
				break
			}
			h.logger.Debugf("read asr stream client frame failed: %v", err)
			sendError(fmt.Sprintf("client stream error: %v", err))
			cancel()
			return
		}

		switch messageType {
		case websocket.BinaryMessage:
			if err := session.SendAudio(payload); err != nil {
				h.logger.Warnf("forward audio chunk failed: %v", err)
				sendError("failed to forward audio chunk")
				cancel()
				return
			}
		case websocket.TextMessage:
			var message map[string]interface{}
			if err := json.Unmarshal(payload, &message); err != nil {
				sendError(fmt.Sprintf("invalid control frame: %v", err))
				continue
			}
			msgType := ""
			if v, ok := message["type"].(string); ok {
				msgType = strings.ToLower(strings.TrimSpace(v))
			}
			switch msgType {
			case "stop", "close":
				finished = true
			case "ping":
				_ = sendJSON(map[string]interface{}{"type": "pong"})
			}
		case websocket.CloseMessage:
			finished = true
		default:
			continue
		}
	}

	stopSession()

	var resultErr error
	select {
	case resultErr = <-results:
	case <-time.After(5 * time.Second):
		resultErr = context.DeadlineExceeded
	}

	if resultErr != nil && !errors.Is(resultErr, context.Canceled) && !errors.Is(resultErr, context.DeadlineExceeded) {
		h.logger.Warnf("asr stream finished with error: %v", resultErr)
		sendError(resultErr.Error())
	}
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

func estimateAudioDurationMs(data []byte, format string, sampleRate int) int {
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
		rate := sampleRate
		if rate <= 0 {
			rate = 16000
		}
		seconds := float64(samples) / float64(rate)
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
