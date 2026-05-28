package logic

import (
	"context"
	"fmt"
	"gamebase/internal/models"
	"strconv"

	einoretriever "github.com/cloudwego/eino/components/retriever"
	einoschema "github.com/cloudwego/eino/schema"
)

type postHybridRetriever struct {
	defaultTopK int
}

func NewPostHybridRetriever(defaultTopK int) *postHybridRetriever {
	if defaultTopK <= 0 {
		defaultTopK = 4
	}
	return &postHybridRetriever{defaultTopK: defaultTopK}
}

func (r *postHybridRetriever) Retrieve(ctx context.Context, query string, opts ...einoretriever.Option) ([]*einoschema.Document, error) {
	options := einoretriever.GetCommonOptions(&einoretriever.Options{}, opts...)
	topK := r.defaultTopK
	if options.TopK != nil && *options.TopK > 0 {
		topK = *options.TopK
	}

	result, err := SearchPostByRAG(ctx, &models.ParamRAGSearch{
		Query: query,
		TopK:  topK,
	})
	if err != nil {
		return nil, err
	}

	docs := make([]*einoschema.Document, 0, len(result.Hits))
	for _, hit := range result.Hits {
		doc := &einoschema.Document{
			ID:      strconv.FormatInt(hit.PostID, 10),
			Content: hit.ChunkText,
			MetaData: map[string]interface{}{
				"post_id":       hit.PostID,
				"title":         hit.Title,
				"content":       hit.Content,
				"chunk_index":   hit.ChunkIndex,
				"chunk_text":    hit.ChunkText,
				"community_id":  hit.CommunityID,
				"author_id":     hit.AuthorID,
				"retrieval_key": fmt.Sprintf("%d_%d", hit.PostID, hit.ChunkIndex),
			},
		}
		doc.WithScore(float64(hit.Score))
		docs = append(docs, doc)
	}
	return docs, nil
}
