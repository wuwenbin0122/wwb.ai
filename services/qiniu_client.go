package services

import (
    "encoding/json"
    "fmt"
    "net/http"
    "strings"
    "time"
)

const qiniuHTTPTimeout = 20 * time.Second

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type qiniuAPIError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type qiniuErrorEnvelope struct {
	Error *qiniuAPIError `json:"error,omitempty"`
}

func newDefaultHTTPClient() *http.Client {
    return &http.Client{Timeout: qiniuHTTPTimeout}
}

// newHTTPClientWithTimeout builds an HTTP client with a custom timeout.
// Falls back to the library default when duration is non-positive.
func newHTTPClientWithTimeout(d time.Duration) *http.Client {
    if d <= 0 {
        d = qiniuHTTPTimeout
    }
    return &http.Client{Timeout: d}
}

func decodeQiniuError(body []byte) *qiniuAPIError {
	if len(body) == 0 {
		return nil
	}

	var envelope qiniuErrorEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil
	}

	if envelope.Error == nil {
		return nil
	}

	envelope.Error.Message = strings.TrimSpace(envelope.Error.Message)
	return envelope.Error
}

func buildQiniuAPIError(statusCode int, body []byte) error {
	if apiErr := decodeQiniuError(body); apiErr != nil {
		if apiErr.Code != "" && apiErr.Message != "" {
			return fmt.Errorf("qiniu api error (%d, %s): %s", statusCode, apiErr.Code, apiErr.Message)
		}
		if apiErr.Message != "" {
			return fmt.Errorf("qiniu api error (%d): %s", statusCode, apiErr.Message)
		}
		if apiErr.Code != "" {
			return fmt.Errorf("qiniu api error (%d, %s)", statusCode, apiErr.Code)
		}
	}

	snippet := strings.TrimSpace(string(body))
	if snippet == "" {
		snippet = http.StatusText(statusCode)
	}
	if len(snippet) > 256 {
		snippet = snippet[:256]
	}

	return fmt.Errorf("qiniu api error (%d): %s", statusCode, snippet)
}
