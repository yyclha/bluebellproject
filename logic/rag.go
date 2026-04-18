package logic

import (
	"bluebell/dao/milvus"
	"bluebell/dao/mysql"
	"bluebell/models"
	"bluebell/pkg/embedder"
	"bluebell/pkg/ragchat"
	"bluebell/setting"
	"context"
	"errors"
	"fmt"
	"strconv"
)

func SearchPostByRAG(ctx context.Context, p *models.ParamRAGSearch) (*models.RAGSearchResult, error) {
	vector, err := embedder.EmbedText(ctx, p.Query)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("embedding stage timed out: %w", err)
		}
		return nil, fmt.Errorf("embedding stage failed: %w", err)
	}

	hits, err := milvus.SearchByVector(ctx, vector, p.TopK)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("milvus search stage timed out: %w", err)
		}
		return nil, fmt.Errorf("milvus search stage failed: %w", err)
	}
	if len(hits) == 0 {
		return &models.RAGSearchResult{Query: p.Query, Hits: []models.RAGHit{}}, nil
	}

	postIDs := make([]string, 0, len(hits))
	seenPostID := make(map[int64]struct{}, len(hits))
	for _, hit := range hits {
		if _, ok := seenPostID[hit.PostID]; ok {
			continue
		}
		seenPostID[hit.PostID] = struct{}{}
		postIDs = append(postIDs, strconv.FormatInt(hit.PostID, 10))
	}

	postList, err := mysql.GetPostListByIDs(postIDs)
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

	postHits := make([]models.RAGHit, 0, len(postIDs))
	added := make(map[int64]struct{}, len(postIDs))
	for _, hit := range hits {
		if _, ok := added[hit.PostID]; ok {
			continue
		}

		post, ok := postMap[hit.PostID]
		if !ok {
			continue
		}

		postHits = append(postHits, models.RAGHit{
			PostID:      post.ID,
			Score:       hit.Score,
			Title:       post.Title,
			Content:     post.Content,
			ChunkIndex:  hit.ChunkIndex,
			ChunkText:   hit.ChunkText,
			CommunityID: post.CommunityID,
			AuthorID:    post.AuthorID,
		})
		added[hit.PostID] = struct{}{}
	}

	return &models.RAGSearchResult{
		Query: p.Query,
		Hits:  postHits,
	}, nil
}

func StreamRAGAssistant(ctx context.Context, p *models.ParamRAGChat, onChunk func(string) error) (*models.RAGChatResult, error) {
	if p == nil {
		return nil, errors.New("rag chat param is nil")
	}
	if !ragchat.Enabled() {
		return nil, errors.New("rag chat is disabled")
	}

	topK := p.TopK
	if topK <= 0 {
		topK = ragchat.TopK()
	}

	searchResult, err := SearchPostByRAG(ctx, &models.ParamRAGSearch{
		Query: p.Question,
		TopK:  topK,
	})
	if err != nil {
		return nil, fmt.Errorf("rag retrieval failed: %w", err)
	}

	answer, err := ragchat.StreamAnswerQuestion(ctx, p.Question, searchResult.Hits, onChunk)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("llm answer stage timed out: %w", err)
		}
		return nil, fmt.Errorf("llm answer stage failed: %w", err)
	}

	return &models.RAGChatResult{
		Question: p.Question,
		Answer:   answer,
		Model:    ragchat.ModelName(),
		Hits:     searchResult.Hits,
	}, nil
}

func IndexPostToRAG(ctx context.Context, p *models.Post) error {
	if !milvus.Enabled() || !embedder.Enabled() {
		return nil
	}

	fullText := BuildPostRAGText(p.Title, p.Content)
	chunks := SplitTextToChunks(fullText, chunkSize(), chunkOverlap())
	if len(chunks) == 0 {
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

func chunkSize() int {
	if setting.Conf != nil && setting.Conf.MilvusConfig != nil && setting.Conf.MilvusConfig.ChunkSize > 0 {
		return setting.Conf.MilvusConfig.ChunkSize
	}
	return defaultChunkSize
}

func chunkOverlap() int {
	if setting.Conf != nil && setting.Conf.MilvusConfig != nil && setting.Conf.MilvusConfig.ChunkOverlap >= 0 {
		return setting.Conf.MilvusConfig.ChunkOverlap
	}
	return defaultChunkOverlap
}
