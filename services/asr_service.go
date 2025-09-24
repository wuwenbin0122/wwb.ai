package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/wuwenbin0122/wwb.ai/config"
	"go.uber.org/zap"
)

// ASRResult represents a single transcription message returned by the ASR service.
type ASRResult struct {
	Text    string          `json:"text"`
	IsFinal bool            `json:"is_final"`
	Raw     json.RawMessage `json:"raw"`
}

// ASRService manages a streaming session with the Qiniu ASR websocket endpoint.
type ASRService struct {
	endpoint string
	dialer   *websocket.Dialer
	logger   *zap.SugaredLogger
}

// NewASRService constructs a new ASRService.
func NewASRService(cfg *config.Config, logger *zap.SugaredLogger) *ASRService {
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second

	return &ASRService{
		endpoint: cfg.QiniuASREndpoint,
		dialer:   &dialer,
		logger:   logger,
	}
}

// StreamRecognize streams audio chunks to the ASR service and emits transcription results via the callback.
//
// The provided audio channel should deliver raw audio frames compatible with the configured ASR model.
// Closing the audio channel signals that no more audio will be sent. The callback is invoked in the
// reader goroutine whenever a new transcription result arrives.
func (s *ASRService) StreamRecognize(
	ctx context.Context,
	token string,
	audio <-chan []byte,
	onResult func(ASRResult),
) error {
	if s.endpoint == "" {
		return errors.New("asr endpoint is not configured")
	}

	if token == "" {
		return errors.New("authorization token is required")
	}

	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)

	conn, _, err := s.dialer.DialContext(ctx, s.endpoint, header)
	if err != nil {
		return fmt.Errorf("dial asr websocket: %w", err)
	}
	defer conn.Close()

	errCh := make(chan error, 1)

	go func() {
		defer func() {
			// ensure the main loop is released even if we exit cleanly
			errCh <- nil
		}()

		for {
			messageType, payload, readErr := conn.ReadMessage()
			if readErr != nil {
				errCh <- fmt.Errorf("read asr message: %w", readErr)
				return
			}

			if messageType != websocket.TextMessage {
				// the service is currently expected to respond with text frames
				continue
			}

			var envelope map[string]interface{}
			if err := json.Unmarshal(payload, &envelope); err != nil {
				s.logger.Warnf("unmarshal asr payload failed: %v", err)
				continue
			}

			text, _ := envelope["text"].(string)
			if text == "" {
				continue
			}

			isFinal, _ := envelope["is_final"].(bool)

			if onResult != nil {
				onResult(ASRResult{
					Text:    text,
					IsFinal: isFinal,
					Raw:     append(json.RawMessage(nil), payload...),
				})
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errCh:
			// nil indicates reader exited without an explicit error (e.g. connection closed)
			if err != nil {
				return err
			}
			return nil
		case chunk, ok := <-audio:
			if !ok {
				// signal completion to the ASR service and return
				if writeErr := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "EOF")); writeErr != nil {
					s.logger.Debugf("send websocket close frame failed: %v", writeErr)
				}
				return nil
			}

			if len(chunk) == 0 {
				continue
			}

			if writeErr := conn.WriteMessage(websocket.BinaryMessage, chunk); writeErr != nil {
				return fmt.Errorf("send audio chunk: %w", writeErr)
			}
		}
	}
}
