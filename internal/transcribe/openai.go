// Package transcribe provides a bounded file-transcription adapter.
package transcribe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

const Model = "gpt-transcribe"
const MaxAudioBytes = 2 << 20
const maxResponseBytes = 1 << 20

// Client does not infer speaker identity or timestamps from the returned text.
// The caller must meter the request and bind it to live-session authority.
type Client struct {
	HTTP   *http.Client
	APIKey string
}

// Transcribe submits one complete WAV file. It returns only a final result;
// partial output is deliberately not exposed as evidence.
func (c Client) Transcribe(ctx context.Context, wav []byte) (string, error) {
	if len(wav) == 0 || len(wav) > MaxAudioBytes {
		return "", errors.New("transcribe: invalid audio size")
	}
	if c.APIKey == "" {
		return "", errors.New("transcribe: API key unavailable")
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", "chunk.wav")
	if err != nil {
		return "", err
	}
	if _, err = part.Write(wav); err != nil {
		return "", err
	}
	if err = form.WriteField("model", Model); err != nil {
		return "", err
	}
	if err = form.Close(); err != nil {
		return "", err
	}
	endpoint := "https://api.openai.com/v1/audio/transcriptions"
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, endpoint, &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", form.FormDataContentType())
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := copyClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("transcribe: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("transcribe: provider returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxResponseBytes {
		return "", errors.New("transcribe: response too large")
	}
	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", errors.New("transcribe: invalid response")
	}
	if err := callCtx.Err(); err != nil {
		return "", err
	}
	return strings.TrimSpace(result.Text), nil
}
