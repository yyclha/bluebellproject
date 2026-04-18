package ragchat

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"bluebell/models"
	"bluebell/setting"

	"github.com/cloudwego/eino-ext/components/model/qwen"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

var (
	cfg       *setting.RAGChatConfig
	chatModel einomodel.BaseChatModel
)

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

func Enabled() bool {
	return cfg != nil && cfg.Enabled && chatModel != nil
}

func ModelName() string {
	if cfg == nil {
		return ""
	}
	return cfg.Model
}

func TopK() int {
	if cfg == nil || cfg.TopK <= 0 {
		return 4
	}
	return cfg.TopK
}

func MaxContextChars() int {
	if cfg == nil || cfg.MaxContextChars <= 0 {
		return 3600
	}
	return cfg.MaxContextChars
}

func AnswerQuestion(ctx context.Context, question string, hits []models.RAGHit) (string, error) {
	return StreamAnswerQuestion(ctx, question, hits, nil)
}

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

func normalizeBaseURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	baseURL = strings.TrimSuffix(baseURL, "/chat/completions")
	if strings.HasSuffix(baseURL, "/v1") {
		return baseURL
	}
	return baseURL + "/v1"
}

func float32Ptr(v float32) *float32 {
	return &v
}
