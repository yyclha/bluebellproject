package logic

import (
	"context"
	"errors"
	"fmt"
	"gamebase/internal/agent/ragchat"
	"gamebase/internal/dao/milvus"
	"gamebase/internal/dao/mysql"
	"gamebase/internal/dao/redis"
	"gamebase/internal/models"
	"gamebase/internal/setting"
	"gamebase/pkg/embedder"
	"strconv"
	"strings"
)

// SearchPostByRAG 执行 RAG 检索并返回命中的帖子片段。
func SearchPostByRAG(ctx context.Context, p *models.ParamRAGSearch) (*models.RAGSearchResult, error) {
	vector, err := embedder.EmbedText(ctx, p.Query)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("embedding stage timed out: %w", err)
		}
		return nil, fmt.Errorf("embedding stage failed: %w", err)
	}

	denseHits, err := milvus.SearchByHybrid(ctx, p.Query, vector, maxInt(p.TopK*3, 12))
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("milvus search stage timed out: %w", err)
		}
		return nil, fmt.Errorf("milvus search stage failed: %w", err)
	}
	postIDs := postIDsFromHits(denseHits, p.TopK)
	if len(postIDs) == 0 {
		return &models.RAGSearchResult{Query: p.Query, Hits: []models.RAGHit{}}, nil
	}

	postIDStrings := make([]string, 0, len(postIDs))
	for _, postID := range postIDs {
		postIDStrings = append(postIDStrings, strconv.FormatInt(postID, 10))
	}

	postList, err := mysql.GetPostListByIDs(postIDStrings)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("mysql post hydration stage timed out: %w", err)
		}
		return nil, fmt.Errorf("mysql post hydration stage failed: %w", err)
	}

	postMap := make(map[int64]*models.Post, len(postList))
	for _, post := range postList {
		postMap[post.ID] = post
	}

	denseByPost := make(map[int64]milvus.SearchHit, len(denseHits))
	for _, hit := range denseHits {
		if _, ok := denseByPost[hit.PostID]; ok {
			continue
		}
		denseByPost[hit.PostID] = hit
	}

	postHits := make([]models.RAGHit, 0, len(postIDs))
	for _, postID := range postIDs {
		post, ok := postMap[postID]
		if !ok {
			continue
		}

		denseHit, hasDense := denseByPost[postID]
		score := float32(0)
		chunkIndex := int64(0)
		chunkText := ""
		if hasDense {
			score = denseHit.Score
			chunkIndex = denseHit.ChunkIndex
			chunkText = denseHit.ChunkText
		}

		postHits = append(postHits, models.RAGHit{
			PostID:      post.ID,
			Score:       score,
			Title:       post.Title,
			Content:     post.Content,
			ChunkIndex:  chunkIndex,
			ChunkText:   chunkText,
			CommunityID: post.CommunityID,
			AuthorID:    post.AuthorID,
		})
	}

	return &models.RAGSearchResult{
		Query: p.Query,
		Hits:  postHits,
	}, nil
}

func postIDsFromHits(hits []milvus.SearchHit, topK int) []int64 {
	if topK <= 0 {
		topK = 5
	}
	result := make([]int64, 0, topK)
	seen := make(map[int64]struct{}, topK)
	for _, hit := range hits {
		if _, ok := seen[hit.PostID]; ok {
			continue
		}
		result = append(result, hit.PostID)
		seen[hit.PostID] = struct{}{}
		if len(result) >= topK {
			break
		}
	}
	return result
}

// StreamRAGAssistant 执行流式 RAG 问答并返回答案结果。
func StreamRAGAssistant(ctx context.Context, p *models.ParamRAGChat, onChunk func(string) error) (*models.RAGChatResult, error) {
	if p == nil {
		return nil, errors.New("rag chat param is nil")
	}
	if !ragchat.Enabled() {
		return nil, errors.New("rag chat is disabled")
	}

	history, err := resolveRAGChatHistory(p)
	if err != nil {
		return nil, fmt.Errorf("resolve rag chat history failed: %w", err)
	}

	topK := p.TopK
	if topK <= 0 {
		topK = ragchat.TopK()
	}

	answer, hits, err := ragchat.StreamAnswerQuestion(ctx, p.Question, history, topK, onChunk)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("llm answer stage timed out: %w", err)
		}
		return nil, fmt.Errorf("llm answer stage failed: %w", err)
	}

	if err := persistRAGChatHistory(p.SessionID, history, p.Question, answer); err != nil {
		return nil, fmt.Errorf("persist rag chat history failed: %w", err)
	}

	return &models.RAGChatResult{
		Question: p.Question,
		Answer:   answer,
		Model:    ragchat.ModelName(),
		Hits:     hits,
	}, nil
}

func resolveRAGChatHistory(p *models.ParamRAGChat) ([]models.RAGChatMessage, error) {
	if p == nil {
		return nil, nil
	}
	if strings.TrimSpace(p.SessionID) == "" {
		return compactRAGChatMessages(p.Messages), nil
	}

	stored, err := redis.GetAssistantSessionMessages(strings.TrimSpace(p.SessionID))
	if err != nil {
		return nil, err
	}
	if len(stored) == 0 {
		return compactRAGChatMessages(p.Messages), nil
	}

	history := make([]models.RAGChatMessage, 0, len(stored))
	for _, item := range stored {
		history = append(history, models.RAGChatMessage{
			Role:    item.Role,
			Content: item.Content,
		})
	}
	return compactRAGChatMessages(history), nil
}

func persistRAGChatHistory(sessionID string, history []models.RAGChatMessage, question string, answer string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}

	next := make([]models.RAGChatMessage, 0, len(history)+2)
	next = append(next, compactRAGChatMessages(history)...)
	next = append(next, models.RAGChatMessage{Role: "user", Content: strings.TrimSpace(question)})
	next = append(next, models.RAGChatMessage{Role: "assistant", Content: strings.TrimSpace(answer)})
	next = compactRAGChatMessages(next)

	stored := make([]redis.AssistantSessionMessage, 0, len(next))
	for _, item := range next {
		stored = append(stored, redis.AssistantSessionMessage{
			Role:    item.Role,
			Content: item.Content,
		})
	}
	return redis.SaveAssistantSessionMessages(sessionID, stored)
}

func compactRAGChatMessages(messages []models.RAGChatMessage) []models.RAGChatMessage {
	compacted := make([]models.RAGChatMessage, 0, len(messages))
	for _, msg := range messages {
		role := strings.ToLower(strings.TrimSpace(msg.Role))
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		if role != "user" && role != "assistant" {
			continue
		}
		compacted = append(compacted, models.RAGChatMessage{
			Role:    role,
			Content: content,
		})
	}
	if len(compacted) > 20 {
		compacted = compacted[len(compacted)-20:]
	}
	return compacted
}

// IndexPostToRAG 将单篇帖子切块、向量化并写入 RAG 索引。
func IndexPostToRAG(ctx context.Context, p *models.Post) error {
	fullText := BuildPostRAGText(p.Title, p.Content)
	chunks := SplitTextToChunks(fullText, chunkSize(), chunkOverlap())
	if len(chunks) == 0 {
		if err := mysql.ReplacePostRAGChunks(p.ID, nil); err != nil {
			return err
		}
		return nil
	}

	persistedChunks := make([]models.PostRAGChunk, 0, len(chunks))
	for _, chunk := range chunks {
		persistedChunks = append(persistedChunks, models.PostRAGChunk{
			PostID:     p.ID,
			ChunkIndex: int64(chunk.Index),
			ChunkText:  chunk.Text,
		})
	}
	if err := mysql.ReplacePostRAGChunks(p.ID, persistedChunks); err != nil {
		return err
	}

	if !milvus.Enabled() || !embedder.Enabled() {
		return nil
	}

	chunkDocs := make([]milvus.ChunkDocument, 0, len(chunks))
	for _, chunk := range chunks {
		vector, err := embedder.EmbedText(ctx, chunk.Text)
		if err != nil {
			return err
		}

		chunkDocs = append(chunkDocs, milvus.ChunkDocument{
			PostID:     p.ID,
			ChunkIndex: int64(chunk.Index),
			ChunkText:  chunk.Text,
			Vector:     vector,
		})
	}

	return milvus.ReplacePostChunks(ctx, p.ID, chunkDocs)
}

// ReindexPostToRAG 批量重建帖子 RAG 索引。
func ReindexPostToRAG(ctx context.Context, limit int) (int, error) {
	posts, err := mysql.GetPostListForRAG(limit)
	if err != nil {
		return 0, err
	}

	okCount := 0
	for _, post := range posts {
		if err := IndexPostToRAG(ctx, post); err == nil {
			okCount++
		}
	}

	return okCount, nil
}

// chunkSize 读取当前配置下的文本切块大小。
func chunkSize() int {
	if setting.Conf != nil && setting.Conf.MilvusConfig != nil && setting.Conf.MilvusConfig.ChunkSize > 0 {
		return setting.Conf.MilvusConfig.ChunkSize
	}
	return defaultChunkSize
}

// chunkOverlap 读取当前配置下的文本切块重叠长度。
func chunkOverlap() int {
	if setting.Conf != nil && setting.Conf.MilvusConfig != nil && setting.Conf.MilvusConfig.ChunkOverlap >= 0 {
		return setting.Conf.MilvusConfig.ChunkOverlap
	}
	return defaultChunkOverlap
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
