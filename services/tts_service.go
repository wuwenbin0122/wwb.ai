package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/wuwenbin0122/wwb.ai/config"
	"go.uber.org/zap"
)

// TTSChunk represents an audio segment emitted by the TTS service.
type TTSChunk struct {
	Audio   []byte          `json:"audio"`
	IsFinal bool            `json:"is_final"`
	Raw     json.RawMessage `json:"raw"`
}

// TTSRequest contains the parameters for a synthesis call.
type TTSRequest struct {
	Text       string
	VoiceType  string
	Encoding   string
	SpeedRatio float64
}

// TTSService manages websocket sessions with the Qiniu TTS endpoint.
type TTSService struct {
	endpoint     string
	defaultVoice string
	dialer       *websocket.Dialer
	logger       *zap.SugaredLogger
}

// NewTTSService constructs a new TTSService instance.
func NewTTSService(cfg *config.Config, logger *zap.SugaredLogger) *TTSService {
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second

	voice := cfg.QiniuTTSVoiceType
	if strings.TrimSpace(voice) == "" {
		voice = "qiniu_zh_female_tmjxxy"
	}

	return &TTSService{
		endpoint:     cfg.QiniuTTSEndpoint,
		defaultVoice: voice,
		dialer:       &dialer,
		logger:       logger,
	}
}

// Synthesize streams a textual request to the TTS endpoint and emits audio chunks via the callback.
func (s *TTSService) Synthesize(
	ctx context.Context,
	token string,
	req TTSRequest,
	onChunk func(TTSChunk),
) error {
	if s.endpoint == "" {
		return errors.New("tts endpoint is not configured")
	}

	if token == "" {
		return errors.New("authorization token is required")
	}

	if strings.TrimSpace(req.Text) == "" {
		return errors.New("tts request text is empty")
	}

	voice := req.VoiceType
	if strings.TrimSpace(voice) == "" {
		voice = s.defaultVoice
	}

	encoding := req.Encoding
	if strings.TrimSpace(encoding) == "" {
		encoding = "mp3"
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
			"text": req.Text,
		},
	}

	msg, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal tts payload: %w", err)
	}

	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)

	conn, _, err := s.dialer.DialContext(ctx, s.endpoint, header)
	if err != nil {
		return fmt.Errorf("dial tts websocket: %w", err)
	}
	defer conn.Close()

	if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
		return fmt.Errorf("send tts request: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		messageType, payloadBytes, readErr := conn.ReadMessage()
		if readErr != nil {
			return fmt.Errorf("read tts message: %w", readErr)
		}

		if messageType != websocket.TextMessage && messageType != websocket.BinaryMessage {
			continue
		}

		var envelope map[string]interface{}
		if err := json.Unmarshal(payloadBytes, &envelope); err != nil {
			s.logger.Warnf("unmarshal tts payload failed: %v", err)
			continue
		}

		data, _ := envelope["data"].(string)
		if data == "" {
			continue
		}

		audioBytes, decodeErr := base64.StdEncoding.DecodeString(data)
		if decodeErr != nil {
			s.logger.Warnf("decode tts audio chunk failed: %v", decodeErr)
			continue
		}

		isFinal, _ := envelope["is_final"].(bool)

		if onChunk != nil {
			onChunk(TTSChunk{
				Audio:   audioBytes,
				IsFinal: isFinal,
				Raw:     append(json.RawMessage(nil), payloadBytes...),
			})
		}

		// termination condition: service signals completion
		status, _ := envelope["status"].(string)
		if strings.EqualFold(status, "completed") || isFinal {
			return nil
		}
	}
}
