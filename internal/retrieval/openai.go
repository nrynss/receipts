package retrieval

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"receipts/internal/providerusage"
)

const DefaultModel = "text-embedding-3-small"
const DefaultDimensions = 1536

// Client returns a vector only in the explicitly specified space. Calls must be
// metered by the caller and bound to its channel runtime context.
type Client struct {
	HTTP   *http.Client
	APIKey string
	Space  Space
}

func (c Client) Embed(ctx context.Context, text string) ([]float64, error) {
	if c.APIKey == "" || c.Space.Model == "" || c.Space.Version == "" || c.Space.Dimensions <= 0 || strings.TrimSpace(text) == "" || len(text) > 8192 {
		return nil, errors.New("retrieval: invalid embedding request")
	}
	body, err := json.Marshal(map[string]any{"model": c.Space.Model, "input": text, "dimensions": c.Space.Dimensions, "encoding_format": "float"})
	if err != nil {
		return nil, err
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, "https://api.openai.com/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := copyClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("retrieval: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("retrieval: provider returned HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 1<<20 {
		return nil, errors.New("retrieval: response too large")
	}
	providerusage.Embedding(ctx, raw)
	var output struct {
		Model string `json:"model"`
		Data  []struct {
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &output) != nil || output.Model != c.Space.Model || len(output.Data) != 1 || output.Data[0].Index != 0 || !ValidVector(output.Data[0].Embedding, c.Space.Dimensions) {
		return nil, errors.New("retrieval: invalid embedding response")
	}
	if err = callCtx.Err(); err != nil {
		return nil, err
	}
	return output.Data[0].Embedding, nil
}
