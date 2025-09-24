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

	"github.com/wuwenbin0122/wwb.ai/config"
	"go.uber.org/zap"
)

// ASRInput captures the audio payload forwarded to Qiniu's ASR REST API.
type ASRInput struct {
	Format string
	URL    string
	Data   []byte
}

// ASRResult represents the simplified transcription result returned by the ASR service.
type ASRResult struct {
	ReqID      string          `json:"reqid"`
	Text       string          `json:"text"`
	DurationMS int             `json:"duration_ms"`
	Raw        json.RawMessage `json:"raw"`
}

type asrService struct {
	baseURL string
	model   string
	client  httpDoer
	logger  *zap.SugaredLogger
}

// ASRService exposes a REST-based transcription workflow.
type ASRService struct {
	inner *asrService
}

// NewASRService constructs an ASR service configured for Qiniu's REST API.
func NewASRService(cfg *config.Config, logger *zap.SugaredLogger) *ASRService {
	base := strings.TrimRight(cfg.QiniuAPIBaseURL, "/")
	if base == "" {
		base = "https://openai.qiniu.com/v1"
	}

	model := strings.TrimSpace(cfg.QiniuASRModel)
	if model == "" {
		model = "asr"
	}

	return &ASRService{
		inner: &asrService{
			baseURL: base,
			model:   model,
			client:  newDefaultHTTPClient(),
			logger:  logger,
		},
	}
}

// Recognize submits the provided audio and returns the transcription text.
func (s *ASRService) Recognize(ctx context.Context, token string, input ASRInput) (*ASRResult, error) {
	return s.inner.recognize(ctx, token, input)
}

func (s *asrService) recognize(ctx context.Context, token string, input ASRInput) (*ASRResult, error) {
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("authorization token is required")
	}

	audio := make(map[string]interface{})
	format := strings.TrimSpace(input.Format)
	if format == "" {
		format = "wav"
	}
	audio["format"] = format

	if trimmedURL := strings.TrimSpace(input.URL); trimmedURL != "" {
		audio["url"] = trimmedURL
	}

	if len(input.Data) > 0 {
		audio["data"] = base64.StdEncoding.EncodeToString(input.Data)
	}

	if _, hasURL := audio["url"]; !hasURL {
		if _, hasData := audio["data"]; !hasData {
			return nil, fmt.Errorf("audio payload must include url or data")
		}
	}

	payload := map[string]interface{}{
		"model": s.model,
		"audio": audio,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal asr payload: %w", err)
	}

	endpoint := s.baseURL + "/voice/asr"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create asr request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call asr api: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read asr response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, buildQiniuAPIError(resp.StatusCode, respBody)
	}

	var envelope asrAPIResponse
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return nil, fmt.Errorf("decode asr response: %w", err)
	}

	if envelope.Error != nil && envelope.Error.Message != "" {
		return nil, fmt.Errorf("qiniu asr error: %s", envelope.Error.Message)
	}

	text := strings.TrimSpace(envelope.Data.Result.Text)
	result := &ASRResult{
		ReqID:      envelope.ReqID,
		Text:       text,
		DurationMS: envelope.Data.AudioInfo.Duration,
		Raw:        json.RawMessage(respBody),
	}

	return result, nil
}

type asrAPIResponse struct {
	ReqID     string         `json:"reqid"`
	Operation string         `json:"operation"`
	Data      asrAPIData     `json:"data"`
	Error     *qiniuAPIError `json:"error,omitempty"`
	Status    string         `json:"status,omitempty"`
	Message   string         `json:"message,omitempty"`
}

type asrAPIData struct {
	AudioInfo asrAudioInfo `json:"audio_info"`
	Result    asrResult    `json:"result"`
}

type asrAudioInfo struct {
	Duration int `json:"duration"`
}

type asrResult struct {
	Additions map[string]interface{} `json:"additions"`
	Text      string                 `json:"text"`
}
