package ragchat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"gamebase/internal/models"
	"gamebase/internal/setting"

	"github.com/cloudwego/eino-ext/components/model/qwen"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	einoretriever "github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

var (
	cfg          *setting.RAGChatConfig
	chatModel    model.BaseChatModel
	chatAgent    *adk.ChatModelAgent
	instruction  = "你是一个简洁的社区知识助手。你必须优先调用 retrieve_posts 工具检索帖子知识片段，再基于检索结果回答。只使用工具返回的知识内容回答。若知识不足，请明确说明知识库信息不足。不要编造事实，不要暴露内部 ID、后端字段名或系统实现细节，使用自然中文回答。"
	toolNameRAG  = "retrieve_posts"
	sessionKeyQ  = "user_question"
	sessionKeyH  = "conversation_history"
	sessionKeyTK = "top_k"
	sessionKeyA  = "agent_answer"
	sessionKeyRH = "retrieved_hits"
)

type retrievePostsArgs struct {
	Query string `json:"query"`
	TopK  int    `json:"top_k"`
}

type retrievePostsResult struct {
	Query string          `json:"query"`
	Hits  []models.RAGHit `json:"hits"`
}

type retrievePostsTool struct {
	retriever einoretriever.Retriever
}

func (t *retrievePostsTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: toolNameRAG,
		Desc: "检索社区帖子知识库，返回与问题相关的帖子标题和内容片段。回答前必须先调用这个工具。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {
				Desc:     "用于检索的查询语句，应结合当前用户问题和必要的上下文指代。",
				Required: true,
				Type:     schema.String,
			},
			"top_k": {
				Desc:     "返回的相关帖子数量上限。",
				Required: false,
				Type:     schema.Integer,
			},
		}),
	}, nil
}

func (t *retrievePostsTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	if t == nil || t.retriever == nil {
		return "", errors.New("retrieve_posts tool is not initialized")
	}

	args := &retrievePostsArgs{}
	if err := json.Unmarshal([]byte(argumentsInJSON), args); err != nil {
		return "", err
	}

	query := strings.TrimSpace(args.Query)
	if query == "" {
		values := adk.GetSessionValues(ctx)
		if raw, ok := values[sessionKeyQ].(string); ok {
			query = strings.TrimSpace(raw)
		}
	}

	topK := args.TopK
	if topK <= 0 {
		values := adk.GetSessionValues(ctx)
		switch value := values[sessionKeyTK].(type) {
		case int:
			topK = value
		case float64:
			topK = int(value)
		}
	}
	if topK <= 0 {
		topK = TopK()
	}

	docs, err := t.retriever.Retrieve(ctx, query, einoretriever.WithTopK(topK))
	if err != nil {
		return "", err
	}

	hits := make([]models.RAGHit, 0, len(docs))
	for _, doc := range docs {
		if doc == nil {
			continue
		}
		meta := doc.MetaData
		hit := models.RAGHit{
			PostID:      int64FromMeta(meta["post_id"]),
			Score:       float32(doc.Score()),
			Title:       stringFromMeta(meta["title"]),
			Content:     stringFromMeta(meta["content"]),
			ChunkIndex:  int64FromMeta(meta["chunk_index"]),
			ChunkText:   stringFromMeta(meta["chunk_text"]),
			CommunityID: int64FromMeta(meta["community_id"]),
			AuthorID:    int64FromMeta(meta["author_id"]),
		}
		hits = append(hits, hit)
	}

	adk.AddSessionValues(ctx, map[string]interface{}{
		sessionKeyRH: hits,
	})

	payload := &retrievePostsResult{
		Query: query,
		Hits:  hits,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func Init(c *setting.RAGChatConfig, retriever einoretriever.Retriever) error {
	cfg = c
	chatModel = nil
	chatAgent = nil
	if cfg == nil || !cfg.Enabled {
		return nil
	}
	if cfg.BaseURL == "" || cfg.Model == "" {
		return errors.New("rag_chat base_url/model is empty")
	}
	if retriever == nil {
		return errors.New("rag_chat retriever is nil")
	}

	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 45 * time.Second
	}

	m, err := qwen.NewChatModel(context.Background(), &qwen.ChatModelConfig{
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

	agent, err := adk.NewChatModelAgent(context.Background(), &adk.ChatModelAgentConfig{
		Name:        "rag_assistant",
		Description: "基于社区帖子知识库进行检索问答的助手。",
		Instruction: instruction,
		Model:       m,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: []tool.BaseTool{
					&retrievePostsTool{retriever: retriever},
				},
			},
		},
		OutputKey:     sessionKeyA,
		MaxIterations: 4,
	})
	if err != nil {
		return fmt.Errorf("init rag chat agent failed: %w", err)
	}

	chatModel = m
	chatAgent = agent
	return nil
}

func Enabled() bool {
	return cfg != nil && cfg.Enabled && chatModel != nil && chatAgent != nil
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

func StreamAnswerQuestion(ctx context.Context, question string, history []models.RAGChatMessage, topK int, onChunk func(string) error) (string, error) {
	if !Enabled() {
		return "", errors.New("rag chat is disabled")
	}
	if strings.TrimSpace(question) == "" {
		return "", errors.New("question is empty")
	}

	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: chatAgent, EnableStreaming: true})

	sessionValues, err := buildSessionValues(question, history, topK)
	if err != nil {
		return "", err
	}

	iter := runner.Run(ctx, buildAgentMessages(question, history), adk.WithSessionValues(sessionValues))

	var answerBuilder strings.Builder
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event == nil {
			continue
		}
		if event.Err != nil {
			return "", event.Err
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}

		output := event.Output.MessageOutput
		if output.Role != schema.Assistant {
			continue
		}

		if output.IsStreaming && output.MessageStream != nil {
			for {
				msg, recvErr := output.MessageStream.Recv()
				if recvErr != nil {
					if errors.Is(recvErr, io.EOF) {
						break
					}
					return "", recvErr
				}
				if msg == nil || msg.Content == "" {
					continue
				}
				answerBuilder.WriteString(msg.Content)
				if onChunk != nil {
					if err := onChunk(msg.Content); err != nil {
						return "", err
					}
				}
			}
			continue
		}

		if output.Message != nil && output.Message.Content != "" {
			answerBuilder.WriteString(output.Message.Content)
			if onChunk != nil {
				if err := onChunk(output.Message.Content); err != nil {
					return "", err
				}
			}
		}
	}

	answer := strings.TrimSpace(answerBuilder.String())
	if answer == "" {
		if value, ok := sessionValues[sessionKeyA].(string); ok {
			answer = strings.TrimSpace(value)
		}
	}
	if answer == "" {
		return "", errors.New("rag chat response is empty")
	}
	return answer, nil
}

func RetrievedHitsFromSession(ctx context.Context) []models.RAGHit {
	values := adk.GetSessionValues(ctx)
	if values == nil {
		return nil
	}
	raw, ok := values[sessionKeyRH]
	if !ok {
		return nil
	}
	hits, ok := raw.([]models.RAGHit)
	if ok {
		return hits
	}
	return nil
}

func stringFromMeta(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	default:
		return ""
	}
}

func int64FromMeta(value interface{}) int64 {
	switch v := value.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	default:
		return 0
	}
}

func buildAgentMessages(question string, history []models.RAGChatMessage) []*schema.Message {
	messages := make([]*schema.Message, 0, len(history)+1)
	start := len(history) - 8
	if start < 0 {
		start = 0
	}
	for _, item := range history[start:] {
		role := strings.ToLower(strings.TrimSpace(item.Role))
		content := strings.TrimSpace(item.Content)
		if content == "" {
			continue
		}
		switch role {
		case "user":
			messages = append(messages, schema.UserMessage(content))
		case "assistant":
			messages = append(messages, schema.AssistantMessage(content, nil))
		}
	}
	messages = append(messages, schema.UserMessage(strings.TrimSpace(question)))
	return messages
}

func buildSessionValues(question string, history []models.RAGChatMessage, topK int) (map[string]interface{}, error) {
	historyText := buildHistoryText(history)
	query := buildContextualQuery(question, history)
	if topK <= 0 {
		topK = TopK()
	}
	values := map[string]interface{}{
		sessionKeyQ:  query,
		sessionKeyH:  historyText,
		sessionKeyTK: topK,
	}
	return values, nil
}

func buildHistoryText(history []models.RAGChatMessage) string {
	var builder strings.Builder
	start := len(history) - 8
	if start < 0 {
		start = 0
	}
	for _, item := range history[start:] {
		role := strings.ToLower(strings.TrimSpace(item.Role))
		content := strings.TrimSpace(item.Content)
		if content == "" {
			continue
		}
		switch role {
		case "user":
			builder.WriteString("User: ")
		case "assistant":
			builder.WriteString("Assistant: ")
		default:
			continue
		}
		builder.WriteString(truncateRunes(content, 500))
		builder.WriteString("\n")
	}
	return strings.TrimSpace(builder.String())
}

func buildContextualQuery(question string, history []models.RAGChatMessage) string {
	parts := make([]string, 0, 7)
	start := len(history) - 6
	if start < 0 {
		start = 0
	}
	for _, item := range history[start:] {
		content := strings.TrimSpace(item.Content)
		if content == "" {
			continue
		}
		parts = append(parts, truncateRunes(content, 260))
	}
	parts = append(parts, strings.TrimSpace(question))
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func normalizeBaseURL(baseURL string) string {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return ""
	}
	return strings.TrimRight(baseURL, "/")
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
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

func float32Ptr(v float32) *float32 {
	return &v
}
