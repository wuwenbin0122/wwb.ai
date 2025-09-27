package services

import (
    "bytes"
    "context"
    "encoding/base64"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "strings"
    "time"

    "github.com/wuwenbin0122/wwb.ai/config"
    "go.uber.org/zap"
)

// TTSRequest encapsulates a synthesis task forwarded to Qiniu.
type TTSRequest struct {
	Text       string
	VoiceType  string
	Encoding   string
	SpeedRatio float64
}

// TTSResult is the simplified response returned to the caller.
type TTSResult struct {
	ReqID    string          `json:"reqid"`
	Audio    []byte          `json:"audio"`
	Duration string          `json:"duration"`
	Raw      json.RawMessage `json:"raw"`
}

// VoiceInfo describes a voice returned by /voice/list.
type VoiceInfo struct {
	VoiceName string `json:"voice_name"`
	VoiceType string `json:"voice_type"`
	URL       string `json:"url"`
	Category  string `json:"category"`
	UpdateMS  int64  `json:"updatetime"`
}

type ttsService struct {
	baseURL       string
	defaultVoice  string
	defaultFormat string
	client        httpDoer
	logger        *zap.SugaredLogger
}

// TTSService exposes convenience wrappers over Qiniu's RESTful TTS API.
type TTSService struct {
	inner *ttsService
}

// NewTTSService constructs a TTSService configured with defaults from cfg.
func NewTTSService(cfg *config.Config, logger *zap.SugaredLogger) *TTSService {
	base := strings.TrimRight(cfg.QiniuAPIBaseURL, "/")
	if base == "" {
		base = "https://openai.qiniu.com/v1"
	}

	voice := strings.TrimSpace(cfg.QiniuTTSVoiceType)
	if voice == "" {
		voice = "qiniu_zh_female_tmjxxy"
	}

	format := strings.TrimSpace(cfg.QiniuTTSFormat)
	if format == "" {
		format = "mp3"
	}

    // TTS responses can be slower; use a longer HTTP timeout to avoid premature 504s.
    ttsHTTPClient := newHTTPClientWithTimeout(60 * time.Second)

    return &TTSService{
        inner: &ttsService{
            baseURL:       base,
            defaultVoice:  voice,
            defaultFormat: format,
            client:        ttsHTTPClient,
            logger:        logger,
        },
    }
}

// Synthesize sends text-to-speech request to Qiniu and returns the synthesized audio bytes.
func (s *TTSService) Synthesize(ctx context.Context, token string, req TTSRequest) (*TTSResult, error) {
	return s.inner.synthesize(ctx, token, req)
}

// ListVoices fetches available TTS voices.
func (s *TTSService) ListVoices(ctx context.Context, token string) ([]VoiceInfo, error) {
	return s.inner.listVoices(ctx, token)
}

func (s *ttsService) synthesize(ctx context.Context, token string, req TTSRequest) (*TTSResult, error) {
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("authorization token is required")
	}

	text := strings.TrimSpace(req.Text)
	if text == "" {
		return nil, fmt.Errorf("tts text cannot be empty")
	}

	voice := strings.TrimSpace(req.VoiceType)
	if voice == "" {
		voice = s.defaultVoice
	}

	encoding := strings.TrimSpace(req.Encoding)
	if encoding == "" {
		encoding = s.defaultFormat
	}

	speed := req.SpeedRatio
	if speed <= 0 {
		speed = 1.0
	}

	payload := map[string]interface{}{
		"audio": map[string]interface{}{
			"voice_type":  voice,
			"encoding":    encoding,
			"speed_ratio": speed,
		},
		"request": map[string]interface{}{
			"text": text,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal tts payload: %w", err)
	}

	endpoint := s.baseURL + "/voice/tts"
	reqHTTP, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create tts request: %w", err)
	}

	reqHTTP.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	reqHTTP.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(reqHTTP)
	if err != nil {
		return nil, fmt.Errorf("call tts api: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read tts response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, buildQiniuAPIError(resp.StatusCode, respBody)
	}

	var envelope ttsAPIResponse
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return nil, fmt.Errorf("decode tts response: %w", err)
	}

	if envelope.Error != nil && envelope.Error.Message != "" {
		return nil, fmt.Errorf("qiniu tts error: %s", envelope.Error.Message)
	}

	if envelope.Data == "" {
		return nil, fmt.Errorf("tts response contained no audio data")
	}

	audio, err := base64.StdEncoding.DecodeString(envelope.Data)
	if err != nil {
		return nil, fmt.Errorf("decode tts audio: %w", err)
	}

	result := &TTSResult{
		ReqID:    envelope.ReqID,
		Audio:    audio,
		Duration: envelope.Addition.Duration,
		Raw:      json.RawMessage(respBody),
	}

	return result, nil
}

func (s *ttsService) listVoices(ctx context.Context, token string) ([]VoiceInfo, error) {
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("authorization token is required")
	}

	endpoint := s.baseURL + "/voice/list"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create voice list request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call voice list api: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read voice list response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, buildQiniuAPIError(resp.StatusCode, body)
	}

	var voices []VoiceInfo
	if err := json.Unmarshal(body, &voices); err != nil {
		return nil, fmt.Errorf("decode voice list response: %w", err)
	}

	return voices, nil
}

type ttsAPIResponse struct {
	ReqID     string         `json:"reqid"`
	Operation string         `json:"operation"`
	Sequence  int            `json:"sequence"`
	Data      string         `json:"data"`
	Addition  ttsAddition    `json:"addition"`
	Error     *qiniuAPIError `json:"error,omitempty"`
}

type ttsAddition struct {
	Duration string `json:"duration"`
}
