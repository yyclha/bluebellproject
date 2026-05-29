package main

import (
	"context"
	"fmt"
	"gamebase/internal/dao/milvus"
	"gamebase/internal/dao/mysql"
	"gamebase/internal/logic"
	"gamebase/internal/models"
	"gamebase/internal/setting"
	"gamebase/pkg/embedder"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	configPath := "./conf/config.yaml"
	if len(os.Args) >= 2 && os.Args[1] != "" {
		configPath = os.Args[1]
	}

	query := "Set 17 Space Gods"
	if len(os.Args) >= 3 && strings.TrimSpace(os.Args[2]) != "" {
		query = strings.TrimSpace(os.Args[2])
	}

	topK := 5
	if len(os.Args) >= 4 && os.Args[3] != "" {
		n, err := strconv.Atoi(os.Args[3])
		if err != nil {
			exitf("invalid topK: %v", err)
		}
		topK = n
	}

	if err := setting.Init(configPath); err != nil {
		exitf("load config failed: %v", err)
	}
	if err := mysql.Init(setting.Conf.MySQLConfig); err != nil {
		exitf("init mysql failed: %v", err)
	}
	defer mysql.Close()

	if err := embedder.Init(setting.Conf.EmbeddingConfig); err != nil {
		exitf("init embedding failed: %v", err)
	}
	if err := milvus.Init(setting.Conf.MilvusConfig); err != nil {
		exitf("init milvus failed: %v", err)
	}
	defer milvus.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	fmt.Printf("query: %q topK=%d\n", query, topK)

	stats, err := milvus.CollectionStats(ctx)
	if err != nil {
		exitf("collection stats failed: %v", err)
	}
	fmt.Printf("collection stats: %v\n", stats)

	samples, err := milvus.SampleChunks(ctx, 3)
	if err != nil {
		exitf("sample chunks failed: %v", err)
	}
	printHits("sample chunks", samples)

	vector, err := embedder.EmbedText(ctx, query)
	if err != nil {
		exitf("embed query failed: %v", err)
	}
	fmt.Printf("embedding dim: %d\n", len(vector))

	denseHits, err := milvus.SearchByVector(ctx, vector, topK)
	if err != nil {
		exitf("dense search failed: %v", err)
	}
	printHits("dense hits", denseHits)

	sparseHits, err := milvus.SearchBySparseText(ctx, query, topK)
	if err != nil {
		exitf("sparse hits failed: %v", err)
	}
	printHits("sparse hits", sparseHits)

	hybridHits, err := milvus.SearchByHybrid(ctx, query, vector, topK)
	if err != nil {
		exitf("hybrid search failed: %v", err)
	}
	printHits("hybrid hits", hybridHits)

	ragResult, err := logic.SearchPostByRAG(ctx, &models.ParamRAGSearch{
		Query: query,
		TopK:  topK,
	})
	if err != nil {
		exitf("rag search failed: %v", err)
	}
	fmt.Printf("rag hits: %d\n", len(ragResult.Hits))
	for i, hit := range ragResult.Hits {
		fmt.Printf("[%d] post_id=%d score=%.6f title=%q chunk=%q\n",
			i+1, hit.PostID, hit.Score, hit.Title, preview(hit.ChunkText, 120))
	}
}

func printHits(label string, hits []milvus.SearchHit) {
	fmt.Printf("%s: %d\n", label, len(hits))
	for i, hit := range hits {
		fmt.Printf("[%d] chunk_id=%s post_id=%d chunk_index=%d score=%.6f text=%q\n",
			i+1, hit.ChunkID, hit.PostID, hit.ChunkIndex, hit.Score, preview(hit.ChunkText, 120))
	}
}

func preview(s string, maxRunes int) string {
	runes := []rune(strings.TrimSpace(s))
	if maxRunes <= 0 || len(runes) <= maxRunes {
		return string(runes)
	}
	return string(runes[:maxRunes]) + "..."
}

func exitf(format string, args ...any) {
	fmt.Printf(format+"\n", args...)
	os.Exit(1)
}
