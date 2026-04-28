package ragchat

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"bluebell/internal/models"
	"bluebell/internal/setting"

	"github.com/cloudwego/eino-ext/components/model/qwen"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

var (
	cfg       *setting.RAGChatConfig
	chatModel einomodel.BaseChatModel
)

// Init 初始化当前模块。
func Init(c *setting.RAGChatConfig) error {
	cfg = c
	chatModel = nil
	if cfg == nil || !cfg.Enabled {
		return nil
	}
	if cfg.BaseURL == "" || cfg.Model == "" {
		return errors.New("rag_chat base_url/model is empty")
	}

	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 45 * time.Second
	}

	model, err := qwen.NewChatModel(context.Background(), &qwen.ChatModelConfig{
		BaseURL:     normalizeBaseURL(cfg.BaseURL),
		APIKey:      cfg.APIKey,
		Timeout:     timeout,
		HTTPClient:  &http.Client{Timeout: timeout},
		Model:       cfg.Model,
		Temperature: float32Ptr(float32(temperature())),
	})
	if err != nil {
		return fmt.Errorf("init rag chat model failed: %w", err)
	}

	chatModel = model
	return nil
}

// Enabled 返回当前组件是否已启用且可正常使用。
func Enabled() bool {
	return cfg != nil && cfg.Enabled && chatModel != nil
}

// ModelName 返回当前使用的模型名称。
func ModelName() string {
	if cfg == nil {
		return ""
	}
	return cfg.Model
}

// TopK 返回 RAG 检索使用的默认 TopK。
func TopK() int {
	if cfg == nil || cfg.TopK <= 0 {
		return 4
	}
	return cfg.TopK
}

// MaxContextChars 返回问答上下文允许的最大字符数。
func MaxContextChars() int {
	if cfg == nil || cfg.MaxContextChars <= 0 {
		return 3600
	}
	return cfg.MaxContextChars
}

// StreamAnswerQuestion 基于检索结果流式生成问答答案。
func StreamAnswerQuestion(ctx context.Context, question string, hits []models.RAGHit, onChunk func(string) error) (string, error) {
	if !Enabled() {
		return "", errors.New("rag chat is disabled")
	}
	if strings.TrimSpace(question) == "" {
		return "", errors.New("question is empty")
	}

	messages := []*schema.Message{
		schema.SystemMessage("You are a concise community knowledge assistant. " +
			"Answer the user's question only using the provided knowledge snippets. " +
			"If the snippets are insufficient, say the knowledge base does not contain enough information. " +
			"Do not fabricate facts. Do not expose internal IDs or backend field names. Use plain Chinese."),
		schema.UserMessage(buildPrompt(question, hits)),
	}

	stream, err := chatModel.Stream(ctx, messages)
	if err != nil {
		return "", err
	}

	defer stream.Close()

	var answerBuilder strings.Builder
	var reasoningBuilder strings.Builder

	for {
		msg, recvErr := stream.Recv()
		if recvErr != nil {
			if errors.Is(recvErr, io.EOF) {
				break
			}
			return "", recvErr
		}
		if msg == nil {
			continue
		}

		if msg.ReasoningContent != "" {
			reasoningBuilder.WriteString(msg.ReasoningContent)
		}
		if msg.Content == "" {
			continue
		}

		answerBuilder.WriteString(msg.Content)
		if onChunk != nil {
			if err := onChunk(msg.Content); err != nil {
				return "", err
			}
		}
	}

	answer := strings.TrimSpace(answerBuilder.String())
	if answer != "" {
		return answer, nil
	}

	reasoning := strings.TrimSpace(reasoningBuilder.String())
	if reasoning != "" {
		if onChunk != nil {
			if err := onChunk(reasoning); err != nil {
				return "", err
			}
		}
		return reasoning, nil
	}

	return "", errors.New("rag chat response is empty")
}

// buildPrompt 构建发给模型的提示词内容。
func buildPrompt(question string, hits []models.RAGHit) string {
	var builder strings.Builder
	builder.WriteString("Question:\n")
	builder.WriteString(strings.TrimSpace(question))
	builder.WriteString("\n\nKnowledge snippets:\n")

	remaining := MaxContextChars()
	if len(hits) == 0 {
		builder.WriteString("No knowledge snippets found.\n")
		return builder.String()
	}

	for index, hit := range hits {
		if remaining <= 0 {
			break
		}
		snippet := strings.TrimSpace(hit.ChunkText)
		if snippet == "" {
			snippet = strings.TrimSpace(hit.Content)
		}
		if snippet == "" {
			continue
		}
		if len(snippet) > remaining {
			snippet = snippet[:remaining]
		}
		builder.WriteString(fmt.Sprintf("[%d] Title: %s\n", index+1, strings.TrimSpace(hit.Title)))
		builder.WriteString(fmt.Sprintf("[%d] Content: %s\n\n", index+1, snippet))
		remaining -= len(snippet)
	}

	builder.WriteString("Requirements:\n")
	builder.WriteString("1. Answer directly in Chinese.\n")
	builder.WriteString("2. If the knowledge is insufficient, state that clearly.\n")
	builder.WriteString("3. When using a snippet, mention the related post title naturally in the answer.\n")
	builder.WriteString("4. Do not expose internal post IDs, raw identifiers, or backend field names in the answer.\n")
	return builder.String()
}

// temperature 返回当前问答模型的采样温度。
func temperature() float64 {
	if cfg == nil {
		return 0.2
	}
	if cfg.Temperature < 0 {
		return 0
	}
	if cfg.Temperature > 2 {
		return 2
	}
	return cfg.Temperature
}

// normalizeBaseURL 规范化模型服务基础地址。
func normalizeBaseURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	baseURL = strings.TrimSuffix(baseURL, "/chat/completions")
	if strings.HasSuffix(baseURL, "/v1") {
		return baseURL
	}
	return baseURL + "/v1"
}

// float32Ptr 返回 float32 值的指针。
func float32Ptr(v float32) *float32 {
	return &v
}
