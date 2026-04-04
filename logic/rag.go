package logic

import (
	"bluebell/dao/milvus"
	"bluebell/dao/mysql"
	"bluebell/models"
	"bluebell/pkg/embedder"
	"bluebell/setting"
	"context"
	"strconv"
)

// SearchPostByRAG 先在 Milvus 中检索最相关的 chunk，再聚合为帖子级结果返回给前端。
func SearchPostByRAG(ctx context.Context, p *models.ParamRAGSearch) (*models.RAGSearchResult, error) {
	vector, err := embedder.EmbedText(ctx, p.Query) // 先将用户查询转换成向量，后续用同一向量去检索 chunk。
	if err != nil {
		return nil, err
	}

	hits, err := milvus.SearchByVector(ctx, vector, p.TopK) // 先召回 chunk 级结果，后面再去重聚合成帖子级结果。
	if err != nil {
		return nil, err
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

	postList, err := mysql.GetPostListByIDs(postIDs) // 用去重后的帖子 ID 批量回表，避免重复查询 MySQL。
	if err != nil {
		return nil, err
	}

	postMap := make(map[int64]*models.Post, len(postList))
	for _, post := range postList {
		postMap[post.ID] = post
	}

	postHits := make([]models.RAGHit, 0, len(postIDs))
	added := make(map[int64]struct{}, len(postIDs))
	for _, hit := range hits {
		if _, ok := added[hit.PostID]; ok {
			continue // 同一帖子只保留分数最高的那个 chunk，确保结果页仍按帖子展示。
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

// IndexPostToRAG 将一篇帖子切成多个 chunk，并把每个 chunk 的向量写入 Milvus。
func IndexPostToRAG(ctx context.Context, p *models.Post) error {
	if !milvus.Enabled() || !embedder.Enabled() {
		return nil // 向量检索能力未开启时直接跳过，不影响主业务流程。
	}

	fullText := BuildPostRAGText(p.Title, p.Content)
	chunks := SplitTextToChunks(fullText, chunkSize(), chunkOverlap())
	if len(chunks) == 0 {
		return nil
	}

	chunkDocs := make([]milvus.ChunkDocument, 0, len(chunks))
	for _, chunk := range chunks {
		vector, err := embedder.EmbedText(ctx, chunk.Text) // 每个 chunk 单独向量化，提升长文本检索精度。
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

	return milvus.ReplacePostChunks(ctx, p.ID, chunkDocs) // 重建同一帖子时先删旧 chunk 再写新 chunk，避免脏数据残留。
}

// ReindexPostToRAG 批量重建帖子向量索引，返回成功处理的帖子数。
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
