package postscore

import (
	"bluebell/internal/models"
	"bluebell/internal/setting"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	cfg        *setting.PostScoreConfig
	httpClient *http.Client
	scoreRE    = regexp.MustCompile(`\b([0-9]{1,3})(?:\.[0-9]+)?\b`)
)

type chatRequest struct {
	Model       string        `json:"model"`
	Temperature float64       `json:"temperature"`
	Stream      bool          `json:"stream"`
	Messages    []chatMessage `json:"messages"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type streamResponse struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

type Result struct {
	Score        float64
	ResponseText string
	Model        string
}

// Init 初始化当前模块。
func Init(c *setting.PostScoreConfig) error {
	cfg = c
	if cfg == nil || !cfg.Enabled {
		return nil
	}
	if cfg.BaseURL == "" || cfg.Model == "" {
		return errors.New("post_score base_url/model is empty")
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

// ScoreWeight 执行。
func ScoreWeight() float64 {
	if cfg == nil || cfg.ScoreWeight <= 0 {
		return 1
	}
	return cfg.ScoreWeight
}

// ModelName 返回当前使用的模型名称。
func ModelName() string {
	if cfg == nil {
		return ""
	}
	return cfg.Model
}

// ScorePost 调用模型为帖子生成 AI 分数。
func ScorePost(ctx context.Context, post *models.Post) (*Result, error) {
	if !Enabled() {
		return nil, errors.New("post score is disabled")
	}
	if post == nil {
		return nil, errors.New("post is nil")
	}

	reqBody := chatRequest{
		Model:       cfg.Model,
		Temperature: 0,
		Stream:      true,
		Messages: []chatMessage{
			{
				Role: "system",
				Content: "You are a community post quality scorer. " +
					"Return only one number between 0 and 100. " +
					"Do not return JSON or explanation.",
			},
			{
				Role:    "user",
				Content: buildPrompt(post),
			},
		},
	}

	raw, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, buildChatURL(cfg.BaseURL), bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("X-DashScope-SSE", "enable")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("post score request failed, status: %d, body: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	content, err := readStreamContent(resp.Body)
	if err != nil {
		return nil, err
	}
	if content == "" {
		return nil, errors.New("post score response is empty")
	}

	score, err := parseScore(content)
	if err != nil {
		return nil, err
	}

	return &Result{
		Score:        score,
		ResponseText: content,
		Model:        cfg.Model,
	}, nil
}

// buildPrompt 构建发给模型的提示词内容。
func buildPrompt(post *models.Post) string {
	return "Score this community post on a 0-100 scale.\n" +
		"Consider content quality, information density, discussion value, title clarity, and readability.\n" +
		"Return only the numeric score.\n\n" +
		"Title:\n" + strings.TrimSpace(post.Title) + "\n\n" +
		"Content:\n" + strings.TrimSpace(post.Content)
}

// buildChatURL 构建聊天补全接口地址。
func buildChatURL(baseURL string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(baseURL, "/chat/completions") {
		return baseURL
	}
	if strings.HasSuffix(baseURL, "/v1") {
		return baseURL + "/chat/completions"
	}
	return baseURL + "/v1/chat/completions"
}

// readStreamContent 读取流式响应中的完整文本内容。
func readStreamContent(body io.Reader) (string, error) {
	reader := bufio.NewReader(body)
	var answerBuilder strings.Builder
	var reasoningBuilder strings.Builder

	for {
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}

		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data:") {
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "[DONE]" {
				break
			}
			if payload != "" {
				var chunk streamResponse
				if unmarshalErr := json.Unmarshal([]byte(payload), &chunk); unmarshalErr == nil {
					for _, choice := range chunk.Choices {
						if choice.Delta.ReasoningContent != "" {
							reasoningBuilder.WriteString(choice.Delta.ReasoningContent)
						}
						if choice.Delta.Content != "" {
							answerBuilder.WriteString(choice.Delta.Content)
						}
					}
				}
			}
		}

		if errors.Is(err, io.EOF) {
			break
		}
	}

	answer := strings.TrimSpace(answerBuilder.String())
	if answer != "" {
		return answer, nil
	}
	return strings.TrimSpace(reasoningBuilder.String()), nil
}

// parseScore 从模型输出中解析帖子分数。
func parseScore(text string) (float64, error) {
	match := scoreRE.FindStringSubmatch(strings.TrimSpace(text))
	if len(match) < 2 {
		return 0, fmt.Errorf("cannot parse score from response: %q", text)
	}

	value, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return 0, err
	}
	if value < 0 {
		value = 0
	}
	if value > 100 {
		value = 100
	}
	return math.Round(value*100) / 100, nil
}
