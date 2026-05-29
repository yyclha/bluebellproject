package ragchat

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gamebase/internal/models"
	"gamebase/internal/setting"

	"github.com/cloudwego/eino-ext/components/model/qwen"
	"github.com/cloudwego/eino/components/model"
	einoretriever "github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

const (
	nodeBuildQuery  = "build_query"
	nodeRetriever   = "retrieve_posts"
	nodeBuildPrompt = "build_prompt"
	nodeGenerate    = "generate_answer"
)

var (
	cfg       *setting.RAGChatConfig
	chatModel model.BaseChatModel
	ragGraph  compose.Runnable[*ragRequest, *schema.Message]
)

type ragRequest struct {
	Question string
	History  []models.RAGChatMessage
	TopK     int
}

type hitsStoreKey struct{}
type requestKey struct{}

type recordingRetriever struct {
	delegate einoretriever.Retriever
}

func (r *recordingRetriever) Retrieve(ctx context.Context, query string, opts ...einoretriever.Option) ([]*schema.Document, error) {
	if r == nil || r.delegate == nil {
		return nil, errors.New("rag retriever is not initialized")
	}

	docs, err := r.delegate.Retrieve(ctx, query, opts...)
	if err != nil {
		return nil, err
	}

	if store, ok := ctx.Value(hitsStoreKey{}).(*[]models.RAGHit); ok && store != nil {
		*store = docsToHits(docs)
	}
	return docs, nil
}

func Init(c *setting.RAGChatConfig, retriever einoretriever.Retriever) error {
	cfg = c
	chatModel = nil
	ragGraph = nil
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

	runner, err := buildRAGGraph(context.Background(), m, &recordingRetriever{delegate: retriever})
	if err != nil {
		return err
	}

	chatModel = m
	ragGraph = runner
	return nil
}

func buildRAGGraph(ctx context.Context, m model.BaseChatModel, retriever einoretriever.Retriever) (compose.Runnable[*ragRequest, *schema.Message], error) {
	graph := compose.NewGraph[*ragRequest, *schema.Message]()

	if err := graph.AddLambdaNode(nodeBuildQuery, compose.InvokableLambda(buildRetrieverQuery)); err != nil {
		return nil, err
	}
	if err := graph.AddRetrieverNode(nodeRetriever, retriever); err != nil {
		return nil, err
	}
	if err := graph.AddLambdaNode(nodeBuildPrompt, compose.InvokableLambda(buildPromptMessages)); err != nil {
		return nil, err
	}
	if err := graph.AddChatModelNode(nodeGenerate, m); err != nil {
		return nil, err
	}

	for _, edge := range [][2]string{
		{compose.START, nodeBuildQuery},
		{nodeBuildQuery, nodeRetriever},
		{nodeRetriever, nodeBuildPrompt},
		{nodeBuildPrompt, nodeGenerate},
		{nodeGenerate, compose.END},
	} {
		if err := graph.AddEdge(edge[0], edge[1]); err != nil {
			return nil, err
		}
	}

	runner, err := graph.Compile(ctx, compose.WithGraphName("gamebase_rag_chat"))
	if err != nil {
		return nil, fmt.Errorf("compile rag chat graph failed: %w", err)
	}
	return runner, nil
}

func Enabled() bool {
	return cfg != nil && cfg.Enabled && chatModel != nil && ragGraph != nil
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

func StreamAnswerQuestion(ctx context.Context, question string, history []models.RAGChatMessage, topK int, onChunk func(string) error) (string, []models.RAGHit, error) {
	if !Enabled() {
		return "", nil, errors.New("rag chat is disabled")
	}
	if strings.TrimSpace(question) == "" {
		return "", nil, errors.New("question is empty")
	}
	if topK <= 0 {
		topK = TopK()
	}

	req := &ragRequest{
		Question: strings.TrimSpace(question),
		History:  history,
		TopK:     topK,
	}
	hits := make([]models.RAGHit, 0, topK)
	runCtx := context.WithValue(ctx, requestKey{}, req)
	runCtx = context.WithValue(runCtx, hitsStoreKey{}, &hits)

	stream, err := ragGraph.Stream(
		runCtx,
		req,
		compose.WithRetrieverOption(einoretriever.WithTopK(topK)).DesignateNode(nodeRetriever),
	)
	if err != nil {
		return "", nil, err
	}
	defer stream.Close()

	var answerBuilder strings.Builder
	for {
		msg, recvErr := stream.Recv()
		if recvErr != nil {
			if errors.Is(recvErr, io.EOF) {
				break
			}
			return "", hits, recvErr
		}
		if msg == nil || msg.Content == "" {
			continue
		}
		answerBuilder.WriteString(msg.Content)
		if onChunk != nil {
			if err := onChunk(msg.Content); err != nil {
				return "", hits, err
			}
		}
	}

	answer := strings.TrimSpace(answerBuilder.String())
	if answer == "" {
		return "", hits, errors.New("rag chat response is empty")
	}
	return answer, hits, nil
}

func buildRetrieverQuery(ctx context.Context, req *ragRequest) (string, error) {
	if req == nil {
		return "", errors.New("rag request is nil")
	}
	parts := make([]string, 0, 7)
	start := len(req.History) - 6
	if start < 0 {
		start = 0
	}
	for _, item := range req.History[start:] {
		content := strings.TrimSpace(item.Content)
		if content == "" {
			continue
		}
		parts = append(parts, truncateRunes(content, 260))
	}
	parts = append(parts, strings.TrimSpace(req.Question))
	return strings.TrimSpace(strings.Join(parts, "\n")), nil
}

func buildPromptMessages(ctx context.Context, docs []*schema.Document) ([]*schema.Message, error) {
	req, _ := ctx.Value(requestKey{}).(*ragRequest)
	if req == nil {
		return nil, errors.New("rag request missing from context")
	}

	messages := make([]*schema.Message, 0, len(req.History)+2)
	messages = append(messages, schema.SystemMessage(buildSystemPrompt(docs)))

	start := len(req.History) - 8
	if start < 0 {
		start = 0
	}
	for _, item := range req.History[start:] {
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
	messages = append(messages, schema.UserMessage(strings.TrimSpace(req.Question)))
	return messages, nil
}

func buildSystemPrompt(docs []*schema.Document) string {
	var builder strings.Builder
	builder.WriteString("You are a concise Chinese community knowledge assistant.\n")
	builder.WriteString("Answer only from the retrieved GameBase post snippets below. ")
	builder.WriteString("If the snippets are insufficient, say that the knowledge base does not contain enough information. ")
	builder.WriteString("Do not invent facts, expose internal IDs, database fields, or backend implementation details.\n\n")
	builder.WriteString("Retrieved snippets:\n")

	if len(docs) == 0 {
		builder.WriteString("No relevant snippets were retrieved.\n")
		return builder.String()
	}

	remaining := MaxContextChars()
	for i, doc := range docs {
		if doc == nil || remaining <= 0 {
			continue
		}
		title := stringFromMeta(doc.MetaData["title"])
		text := strings.TrimSpace(doc.Content)
		if text == "" {
			text = stringFromMeta(doc.MetaData["chunk_text"])
		}
		if text == "" {
			text = stringFromMeta(doc.MetaData["content"])
		}
		if text == "" {
			continue
		}

		item := fmt.Sprintf("[%d] Title: %s\nSnippet: %s\n", i+1, title, text)
		if len([]rune(item)) > remaining {
			item = truncateRunes(item, remaining)
		}
		builder.WriteString(item)
		remaining -= len([]rune(item))
	}
	return builder.String()
}

func docsToHits(docs []*schema.Document) []models.RAGHit {
	hits := make([]models.RAGHit, 0, len(docs))
	for _, doc := range docs {
		if doc == nil {
			continue
		}
		meta := doc.MetaData
		hits = append(hits, models.RAGHit{
			PostID:      int64FromMeta(meta["post_id"]),
			Score:       float32(doc.Score()),
			Title:       stringFromMeta(meta["title"]),
			Content:     stringFromMeta(meta["content"]),
			ChunkIndex:  int64FromMeta(meta["chunk_index"]),
			ChunkText:   stringFromMeta(meta["chunk_text"]),
			CommunityID: int64FromMeta(meta["community_id"]),
			AuthorID:    int64FromMeta(meta["author_id"]),
		})
	}
	return hits
}

func stringFromMeta(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
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
	case int32:
		return int64(v)
	case uint:
		return int64(v)
	case uint64:
		return int64(v)
	case float64:
		return int64(v)
	case string:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	default:
		return 0
	}
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
