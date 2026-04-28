package embedder

import (
	"bluebell/internal/setting"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var (
	cfg        *setting.EmbeddingConfig
	httpClient *http.Client
)

type embeddingRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type embeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// Init 初始化当前模块。
func Init(c *setting.EmbeddingConfig) error {
	cfg = c
	if cfg == nil || !cfg.Enabled {
		return nil
	}
	if cfg.BaseURL == "" || cfg.Model == "" {
		return errors.New("embedding base_url/model is empty")
	}
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	httpClient = &http.Client{Timeout: timeout}
	return nil
}

// Enabled 返回当前组件是否已启用且可正常使用。
func Enabled() bool {
	return cfg != nil && cfg.Enabled
}

// EmbedText 将文本转换为向量表示。
func EmbedText(ctx context.Context, text string) ([]float32, error) {
	if !Enabled() {
		return nil, errors.New("embedding is disabled")
	}
	reqBody := embeddingRequest{
		Model: cfg.Model,
		Input: text,
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	url := buildEmbeddingsURL(cfg.BaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("embedding request failed, status: %d, body: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var embResp embeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&embResp); err != nil {
		return nil, err
	}
	if len(embResp.Data) == 0 || len(embResp.Data[0].Embedding) == 0 {
		return nil, errors.New("embedding response is empty")
	}
	return embResp.Data[0].Embedding, nil
}

// buildEmbeddingsURL 构建 Embedding 接口地址。
func buildEmbeddingsURL(baseURL string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(baseURL, "/embeddings") {
		return baseURL
	}
	if strings.HasSuffix(baseURL, "/v1") {
		return baseURL + "/embeddings"
	}
	return baseURL + "/v1/embeddings"
}
