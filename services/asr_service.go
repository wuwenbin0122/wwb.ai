package services

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
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
	wsURL   string
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

	wsURL := deriveWebsocketURL(base)

	return &ASRService{
		inner: &asrService{
			baseURL: base,
			model:   model,
			client:  newDefaultHTTPClient(),
			logger:  logger,
			wsURL:   wsURL,
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

	format := strings.TrimSpace(input.Format)
	if format == "" {
		format = "wav"
	}
	if trimmedURL := strings.TrimSpace(input.URL); trimmedURL != "" {
		return s.recognizeREST(ctx, token, map[string]interface{}{
			"format": format,
			"url":    trimmedURL,
		})
	}

	if len(input.Data) == 0 {
		return nil, fmt.Errorf("audio payload must include url or inline data")
	}

	result, err := s.recognizeWebsocket(ctx, token, format, input.Data)
	if err == nil {
		return result, nil
	}

	s.logger.Warnf("asr websocket failed, falling back to REST: %v", err)

	restAudio := map[string]interface{}{
		"format": format,
		"data":   base64.StdEncoding.EncodeToString(input.Data),
	}

	restResult, restErr := s.recognizeREST(ctx, token, restAudio)
	if restErr == nil {
		return restResult, nil
	}

	return nil, fmt.Errorf("asr websocket failed: %v; rest fallback failed: %w", err, restErr)
}

func (s *asrService) recognizeREST(ctx context.Context, token string, audio map[string]interface{}) (*ASRResult, error) {
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

func (s *asrService) recognizeWebsocket(ctx context.Context, token, format string, data []byte) (result *ASRResult, err error) {
	if s.wsURL == "" {
		return nil, errors.New("asr websocket endpoint is not configured")
	}

	pcm, sampleRate, channels, bits, err := extractPCMBuffer(format, data)
	if err != nil {
		return nil, err
	}
	if len(pcm) == 0 {
		return nil, errors.New("pcm payload is empty")
	}

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+strings.TrimSpace(token))

	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, resp, err := dialer.DialContext(ctx, s.wsURL+"/voice/asr", headers)
	if err != nil {
		if resp != nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			snippet := strings.TrimSpace(string(body))
			if snippet == "" {
				snippet = resp.Status
			}
			return nil, fmt.Errorf("dial asr websocket: status %d: %s", resp.StatusCode, snippet)
		}
		return nil, fmt.Errorf("dial asr websocket: %w", err)
	}
	defer conn.Close()

	defer func() {
		if r := recover(); r != nil {
			s.logger.Warnf("asr websocket panic recovered: %v", r)
			if err == nil {
				err = fmt.Errorf("asr websocket panic: %v", r)
			}
			result = nil
		}
	}()

	writer := newQiniuASRWebsocketWriter(conn, s.logger, sampleRate, channels, bits)
	if err := writer.SendConfig(s.model); err != nil {
		return nil, fmt.Errorf("send asr config: %w", err)
	}

	bytesPerSample := bits / 8
	if bytesPerSample <= 0 {
		bytesPerSample = 2
	}
	if channels <= 0 {
		channels = 1
	}
	frameSize := sampleRate / 10 * bytesPerSample * channels
	if frameSize <= 0 {
		frameSize = 3200
	}

	audioDuration := time.Duration(0)
	if sampleRate > 0 && bytesPerSample > 0 && channels > 0 {
		samples := float64(len(pcm)) / float64(bytesPerSample*channels)
		seconds := samples / float64(sampleRate)
		if seconds > 0 {
			audioDuration = time.Duration(seconds * float64(time.Second))
		}
	}

	maxWait := 90 * time.Second
	if audioDuration > 0 {
		calculated := time.Duration(float64(audioDuration)*1.5) + 30*time.Second
		if calculated > maxWait {
			maxWait = calculated
		}
	}
	if maxWait > 5*time.Minute {
		maxWait = 5 * time.Minute
	}

	for start := 0; start < len(pcm); start += frameSize {
		end := start + frameSize
		if end > len(pcm) {
			end = len(pcm)
		}
		chunk := pcm[start:end]
		if err := writer.SendAudioChunk(chunk); err != nil {
			return nil, fmt.Errorf("send audio chunk: %w", err)
		}
	}

	if err := writer.SendStop(); err != nil {
		s.logger.Debugf("send asr stop frame failed: %v", err)
	}

	var finalText string
	var reqID string
	var raw json.RawMessage
	var durationMS int

	start := time.Now()

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		remaining := maxWait - time.Since(start)
		if remaining <= 0 {
			return nil, errors.New("asr websocket timed out waiting for response")
		}
		_ = conn.SetReadDeadline(time.Now().Add(remaining))

		messageType, payload, readErr := conn.ReadMessage()
		if readErr != nil {
			if websocket.IsCloseError(readErr, websocket.CloseNormalClosure, websocket.CloseGoingAway) || errors.Is(readErr, io.EOF) {
				break
			}

			var netErr net.Error
			if errors.As(readErr, &netErr) && netErr.Timeout() {
				if finalText != "" {
					break
				}
				elapsed := time.Since(start)
				return nil, fmt.Errorf("asr websocket timed out waiting for response after %s: %w", elapsed.Truncate(time.Second), readErr)
			}

			if finalText != "" {
				break
			}
			return nil, fmt.Errorf("read asr message: %w", readErr)
		}

		var envelope map[string]interface{}
		var processedPayload []byte
		if messageType == websocket.BinaryMessage {
			env, buf, err := parseASRWSBinaryMessage(payload)
			if err != nil {
				s.logger.Debugf("parse asr binary message failed: %v", err)
				continue
			}
			envelope = env
			processedPayload = buf
		} else if messageType == websocket.TextMessage {
			env := make(map[string]interface{})
			if err := json.Unmarshal(payload, &env); err != nil {
				s.logger.Debugf("unmarshal asr text payload failed: %v", err)
				continue
			}
			envelope = env
			processedPayload = append([]byte(nil), payload...)
		} else {
			continue
		}

		raw = append(raw[:0], processedPayload...)

		if errField, ok := envelope["error"].(map[string]interface{}); ok {
			if message, ok := errField["message"].(string); ok && strings.TrimSpace(message) != "" {
				return nil, fmt.Errorf("qiniu asr error: %s", message)
			}
		}

		if v, ok := envelope["reqid"].(string); ok && v != "" {
			reqID = v
		}

		if dataObj, ok := envelope["data"].(map[string]interface{}); ok {
			if resultObj, ok := dataObj["result"].(map[string]interface{}); ok {
				if text, ok := resultObj["text"].(string); ok && text != "" {
					finalText = text
				}
				if additions, ok := resultObj["additions"].(map[string]interface{}); ok {
					if durStr, ok := additions["duration"].(string); ok {
						if val, err := strconv.Atoi(strings.TrimSpace(durStr)); err == nil {
							durationMS = val
						}
					}
				}
			}
			if audioInfo, ok := dataObj["audio_info"].(map[string]interface{}); ok && durationMS == 0 {
				if dur, ok := audioInfo["duration"].(float64); ok {
					durationMS = int(dur)
				}
			}
		}

		if text, ok := envelope["text"].(string); ok && text != "" {
			finalText = text
		}

		isFinal := false
		if v, ok := envelope["is_final"]; ok {
			switch t := v.(type) {
			case bool:
				isFinal = t
			case string:
				isFinal = strings.EqualFold(t, "true")
			}
		}

		if isFinal && finalText != "" {
			break
		}

		if time.Since(start) >= maxWait {
			break
		}
	}

	if durationMS == 0 {
		samples := float64(len(pcm)) / 2.0
		durationMS = int(math.Round(samples / 16000.0 * 1000.0))
	}

	cleanText := strings.TrimSpace(finalText)
	if cleanText == "" {
		return nil, errors.New("asr websocket produced no transcription")
	}

	envelope := map[string]interface{}{
		"reqid":     reqID,
		"operation": "asr",
		"data": map[string]interface{}{
			"audio_info": map[string]interface{}{"duration": durationMS},
			"result": map[string]interface{}{
				"additions": map[string]interface{}{"duration": strconv.Itoa(durationMS)},
				"text":      cleanText,
			},
		},
	}

	serialized, marshalErr := json.Marshal(envelope)
	if marshalErr != nil {
		s.logger.Debugf("marshal websocket envelope failed: %v", marshalErr)
		serialized = append([]byte(nil), raw...)
	}

	return &ASRResult{
		ReqID:      reqID,
		Text:       cleanText,
		DurationMS: durationMS,
		Raw:        json.RawMessage(serialized),
	}, nil
}

func extractPCMBuffer(format string, data []byte) ([]byte, int, int, int, error) {
	format = strings.ToLower(strings.TrimSpace(format))
	switch {
	case format == "" || format == "wav" || format == "wave" || strings.HasSuffix(format, "/wav") || strings.HasSuffix(format, "/wave"):
		if len(data) <= 44 {
			return nil, 0, 0, 0, fmt.Errorf("wav payload too short")
		}
		channels := int(binary.LittleEndian.Uint16(data[22:24]))
		if channels <= 0 {
			channels = 1
		}
		sampleRate := int(binary.LittleEndian.Uint32(data[24:28]))
		if sampleRate <= 0 {
			sampleRate = 16000
		}
		bits := int(binary.LittleEndian.Uint16(data[34:36]))
		if bits <= 0 {
			bits = 16
		}
		return data[44:], sampleRate, channels, bits, nil
	case format == "pcm" || strings.Contains(format, "pcm"):
		return data, 16000, 1, 16, nil
	default:
		return nil, 0, 0, 0, fmt.Errorf("unsupported inline audio format: %s", format)
	}
}

func deriveWebsocketURL(base string) string {
	trimmed := strings.TrimSpace(base)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "http://") {
		return "ws://" + strings.TrimPrefix(trimmed, "http://")
	}
	if strings.HasPrefix(trimmed, "https://") {
		return "wss://" + strings.TrimPrefix(trimmed, "https://")
	}
	if strings.HasPrefix(trimmed, "ws://") || strings.HasPrefix(trimmed, "wss://") {
		return trimmed
	}
	return "wss://" + trimmed
}

type qiniuASRWebsocketWriter struct {
	conn       *websocket.Conn
	logger     *zap.SugaredLogger
	seq        uint32
	sampleRate int
	channels   int
	bits       int
}

func newQiniuASRWebsocketWriter(conn *websocket.Conn, logger *zap.SugaredLogger, sampleRate, channels, bits int) *qiniuASRWebsocketWriter {
	if sampleRate <= 0 {
		sampleRate = 16000
	}
	if channels <= 0 {
		channels = 1
	}
	if bits <= 0 {
		bits = 16
	}
	return &qiniuASRWebsocketWriter{
		conn:       conn,
		logger:     logger,
		sampleRate: sampleRate,
		channels:   channels,
		bits:       bits,
		seq:        1,
	}
}

func (w *qiniuASRWebsocketWriter) SendConfig(model string) error {
	req := map[string]interface{}{
		"user": map[string]interface{}{
			"uid": uuid.NewString(),
		},
		"audio": map[string]interface{}{
			"format":      "pcm",
			"sample_rate": w.sampleRate,
			"bits":        w.bits,
			"channel":     w.channels,
			"codec":       "raw",
		},
		"request": map[string]interface{}{
			"model_name":  model,
			"enable_punc": true,
		},
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return err
	}

	return w.sendFrame(1, payload, true)
}

func (w *qiniuASRWebsocketWriter) SendAudioChunk(chunk []byte) error {
	if len(chunk) == 0 {
		return nil
	}
	return w.sendFrame(2, chunk, true)
}

func (w *qiniuASRWebsocketWriter) SendStop() error {
	return w.sendFrame(4, nil, false)
}

func (w *qiniuASRWebsocketWriter) sendFrame(messageType byte, payload []byte, compress bool) error {
	compressed := payload
	compressionFlag := byte(0)
	if compress {
		var err error
		compressed, err = gzipCompress(payload)
		if err != nil {
			return err
		}
		compressionFlag = 0x01
	}

	header := []byte{(1 << 4) | 1, (messageType << 4) | 1, (1 << 4) | compressionFlag, 0}
	seqBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(seqBytes, w.seq)
	w.seq++

	lengthBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(lengthBytes, uint32(len(compressed)))

	frame := make([]byte, 0, len(header)+len(seqBytes)+len(lengthBytes)+len(compressed))
	frame = append(frame, header...)
	frame = append(frame, seqBytes...)
	frame = append(frame, lengthBytes...)
	frame = append(frame, compressed...)

	return w.conn.WriteMessage(websocket.BinaryMessage, frame)
}

func parseASRWSBinaryMessage(data []byte) (map[string]interface{}, []byte, error) {
	if len(data) < 4 {
		return nil, nil, errors.New("binary message too short")
	}
	headerSize := int(data[0] & 0x0F)
	if headerSize <= 0 {
		headerSize = 1
	}
	baseOffset := headerSize * 4
	if len(data) < baseOffset {
		return nil, nil, errors.New("invalid header size")
	}
	flags := data[1] & 0x0F
	messageType := data[1] >> 4
	serialization := data[2] >> 4
	compression := data[2] & 0x0F

	payload := data[baseOffset:]
	if flags&0x01 == 0x01 {
		if len(payload) < 4 {
			return nil, nil, errors.New("payload missing sequence")
		}
		payload = payload[4:]
	}

	if messageType == 0x09 && len(payload) >= 4 {
		size := int(binary.BigEndian.Uint32(payload[:4]))
		if size <= len(payload)-4 {
			payload = payload[4 : 4+size]
		} else {
			return nil, nil, errors.New("payload size mismatch")
		}
	}

	if compression == 0x01 {
		decompressed, err := gzipDecompress(payload)
		if err != nil {
			return nil, nil, err
		}
		payload = decompressed
	}

	if serialization == 0x01 {
		var envelope map[string]interface{}
		if err := json.Unmarshal(payload, &envelope); err != nil {
			return nil, nil, err
		}
		return envelope, append([]byte(nil), payload...), nil
	}

	envelope := map[string]interface{}{
		"text": string(payload),
	}
	return envelope, append([]byte(nil), payload...), nil
}

func gzipCompress(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return []byte{}, nil
	}
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	if _, err := writer.Write(data); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func gzipDecompress(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, reader); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
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
