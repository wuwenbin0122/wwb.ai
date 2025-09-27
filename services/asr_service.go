package services

import (
    "bytes"
    "compress/gzip"
    "context"
    "encoding/binary"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "strings"

    "github.com/gorilla/websocket"
    "github.com/wuwenbin0122/wwb.ai/config"
    "github.com/wuwenbin0122/wwb.ai/db/models"
    "go.uber.org/zap"
)

// ASRInput captures the audio payload forwarded to Qiniu's ASR REST API.
type ASRInput struct {
    Format string
    URL    string
    Data   []byte // ignored in REST-only implementation
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
type ASRService struct { inner *asrService }

// NewASRService constructs an ASR service configured for Qiniu's REST API.
func NewASRService(cfg *config.Config, logger *zap.SugaredLogger) *ASRService {
    base := strings.TrimRight(cfg.QiniuAPIBaseURL, "/")
    if base == "" { base = "https://openai.qiniu.com/v1" }
    model := strings.TrimSpace(cfg.QiniuASRModel)
    if model == "" { model = "asr" }
    return &ASRService{ inner: &asrService{ baseURL: base, model: model, client: newDefaultHTTPClient(), logger: logger } }
}

// Recognize submits the provided audio (by URL) and returns the transcription text.
func (s *ASRService) Recognize(ctx context.Context, token string, input ASRInput) (*ASRResult, error) {
    return s.inner.recognizeREST(ctx, token, input)
}

func (s *asrService) recognizeREST(ctx context.Context, token string, input ASRInput) (*ASRResult, error) {
    token = strings.TrimSpace(token)
    if token == "" { return nil, fmt.Errorf("authorization token is required") }

    format := strings.TrimSpace(input.Format)
    if format == "" { format = "mp3" }

    url := strings.TrimSpace(input.URL)
    if url == "" { return nil, fmt.Errorf("audio_url is required for ASR REST") }

    payload := map[string]interface{}{
        "model": s.model,
        "audio": map[string]interface{}{ "format": format, "url": url },
    }

    body, err := json.Marshal(payload)
    if err != nil { return nil, fmt.Errorf("marshal asr payload: %w", err) }

    endpoint := s.baseURL + "/voice/asr"
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
    if err != nil { return nil, fmt.Errorf("create asr request: %w", err) }
    req.Header.Set("Authorization", "Bearer "+token)
    req.Header.Set("Content-Type", "application/json")

    resp, err := s.client.Do(req)
    if err != nil { return nil, fmt.Errorf("call asr api: %w", err) }
    defer resp.Body.Close()

    respBody, err := io.ReadAll(resp.Body)
    if err != nil { return nil, fmt.Errorf("read asr response: %w", err) }
    if resp.StatusCode < 200 || resp.StatusCode >= 300 { return nil, buildQiniuAPIError(resp.StatusCode, respBody) }

    var envelope asrAPIResponse
    if err := json.Unmarshal(respBody, &envelope); err != nil { return nil, fmt.Errorf("decode asr response: %w", err) }
    if envelope.Error != nil && envelope.Error.Message != "" { return nil, fmt.Errorf("qiniu asr error: %s", envelope.Error.Message) }

    text := strings.TrimSpace(envelope.Data.Result.Text)
    return &ASRResult{ ReqID: envelope.ReqID, Text: text, DurationMS: envelope.Data.AudioInfo.Duration, Raw: json.RawMessage(respBody) }, nil
}

// response envelopes (mirror previous implementation)
type asrAPIResponse struct {
    ReqID     string         `json:"reqid"`
    Operation string         `json:"operation"`
    Data      asrAPIData     `json:"data"`
    Error     *qiniuAPIError `json:"error,omitempty"`
    Status    string         `json:"status,omitempty"`
    Message   string         `json:"message,omitempty"`
}

type asrAPIData struct { AudioInfo asrAudioInfo `json:"audio_info"`; Result asrResult `json:"result"` }
type asrAudioInfo struct{ Duration int `json:"duration"` }
type asrResult struct{ Additions map[string]interface{} `json:"additions"`; Text string `json:"text"` }

// RaylibASRConfig defines runtime knobs for the raylib-based ASR loop (shared type).
type RaylibASRConfig struct {
    SampleRate       int
    Channels         int
    Bits             int
    SilenceThreshold float64 // RMS 0..1
    SilenceMs        int
    DeviceHint       string // pick device name containing this substring
}

// RunRaylibASR is available only when built with -tags raylib.
// The default build provides a stub that returns an informative error.
func RunRaylibASR(ctx context.Context, cfg *config.Config, nlp *NLPService, role models.Role, lang string, rc RaylibASRConfig, logger *zap.SugaredLogger) error {
    return fmt.Errorf("RunRaylibASR requires build tag 'raylib' (-tags raylib)")
}

// ---- WebSocket helpers (moved from asr_service_raylib.go) ----

// DeriveWebsocketURL builds a ws(s) URL from the base HTTP endpoint.
func DeriveWebsocketURL(base string) string {
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

type ASRWSWriter struct {
    conn       *websocket.Conn
    logger     *zap.SugaredLogger
    seq        uint32
    sampleRate int
    channels   int
    bits       int
}

func NewASRWSWriter(conn *websocket.Conn, logger *zap.SugaredLogger, sampleRate, channels, bits int) *ASRWSWriter {
    if sampleRate <= 0 {
        sampleRate = 16000
    }
    if channels <= 0 {
        channels = 1
    }
    if bits <= 0 {
        bits = 16
    }
    return &ASRWSWriter{conn: conn, logger: logger, seq: 1, sampleRate: sampleRate, channels: channels, bits: bits}
}

func (w *ASRWSWriter) SendConfig(model string) error {
    req := map[string]interface{}{
        "user": map[string]interface{}{"uid": "local"},
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

func (w *ASRWSWriter) SendAudioChunk(chunk []byte) error {
    if len(chunk) == 0 {
        return nil
    }
    return w.sendFrame(2, chunk, true)
}

func (w *ASRWSWriter) SendStop() error { return w.sendFrame(4, nil, false) }

func (w *ASRWSWriter) sendFrame(messageType byte, payload []byte, compress bool) error {
    compressed := payload
    compressionFlag := byte(0)
    if compress {
        var err error
        var buf bytes.Buffer
        gz := gzip.NewWriter(&buf)
        if _, err = gz.Write(payload); err != nil {
            return err
        }
        if err = gz.Close(); err != nil {
            return err
        }
        compressed = buf.Bytes()
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

// ParseASRWSMessage parses a Qiniu ASR WS binary response into a generic envelope and raw JSON payload if present.
func ParseASRWSMessage(data []byte) (map[string]interface{}, []byte, error) {
    if len(data) < 4 {
        return nil, nil, fmt.Errorf("binary message too short")
    }
    headerSize := int(data[0] & 0x0F)
    if headerSize <= 0 {
        headerSize = 1
    }
    baseOffset := headerSize * 4
    if len(data) < baseOffset {
        return nil, nil, fmt.Errorf("invalid header size")
    }
    flags := data[1] & 0x0F
    messageType := data[1] >> 4
    serialization := data[2] >> 4
    compression := data[2] & 0x0F

    payload := data[baseOffset:]
    if flags&0x01 == 0x01 {
        if len(payload) < 4 {
            return nil, nil, fmt.Errorf("payload missing sequence")
        }
        payload = payload[4:]
    }
    if messageType == 0x09 && len(payload) >= 4 {
        size := int(binary.BigEndian.Uint32(payload[:4]))
        if size <= len(payload)-4 {
            payload = payload[4 : 4+size]
        } else {
            return nil, nil, fmt.Errorf("payload size mismatch")
        }
    }
    if compression == 0x01 {
        zr, err := gzip.NewReader(bytes.NewReader(payload))
        if err != nil {
            return nil, nil, err
        }
        var buf bytes.Buffer
        if _, err := io.Copy(&buf, zr); err != nil {
            return nil, nil, err
        }
        _ = zr.Close()
        payload = buf.Bytes()
    }
    if serialization == 0x01 {
        var envelope map[string]interface{}
        if err := json.Unmarshal(payload, &envelope); err != nil {
            return nil, nil, err
        }
        return envelope, append([]byte(nil), payload...), nil
    }
    envelope := map[string]interface{}{"text": string(payload)}
    return envelope, append([]byte(nil), payload...), nil
}
