package milvus

import (
	"bluebell/setting"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

const (
	fieldChunkID   = "chunk_id"
	fieldPostID    = "post_id"
	fieldChunkIdx  = "chunk_index"
	fieldChunkText = "chunk_text"
	fieldEmbedding = "embedding"
)

// ChunkDocument 表示一条需要写入 Milvus 的 chunk 文档记录。
type ChunkDocument struct {
	PostID     int64
	ChunkIndex int64
	ChunkText  string
	Vector     []float32
}

// SearchHit 表示 Milvus 返回的一条 chunk 级命中结果。
type SearchHit struct {
	ChunkID    string
	PostID     int64
	ChunkIndex int64
	ChunkText  string
	Score      float32
}

var (
	cli    client.Client
	cfg    *setting.MilvusConfig
	loaded bool
)

// Init 初始化 Milvus 客户端，并确保目标 collection 已存在且已加载到内存中。
func Init(c *setting.MilvusConfig) error {
	cfg = c
	if cfg == nil || !cfg.Enabled {
		return nil
	}
	if cfg.Address == "" || cfg.Collection == "" || cfg.Dimension <= 0 {
		return errors.New("invalid milvus config")
	}

	var lastErr error
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		var err error
		cli, err = client.NewGrpcClient(ctx, cfg.Address)
		if err == nil {
			err = ensureCollection(ctx)
		}
		if err == nil {
			err = cli.LoadCollection(ctx, cfg.Collection, false)
		}
		cancel()
		if err == nil {
			loaded = true
			return nil
		}
		lastErr = err
		time.Sleep(3 * time.Second)
	}
	return lastErr
}

// Close 关闭 Milvus 客户端连接。
func Close() {
	if cli != nil {
		_ = cli.Close()
	}
}

// Enabled 判断当前 Milvus 能力是否已经正确初始化。
func Enabled() bool {
	return cfg != nil && cfg.Enabled && cli != nil && loaded
}

// ReplacePostChunks 删除帖子旧的 chunk，再把新的 chunk 全量写入 Milvus。
func ReplacePostChunks(ctx context.Context, postID int64, docs []ChunkDocument) error {
	if !Enabled() {
		return errors.New("milvus is disabled")
	}

	if err := deletePostChunks(ctx, postID); err != nil {
		return err
	}
	if len(docs) == 0 {
		return nil
	}

	chunkIDs := make([]string, 0, len(docs))
	postIDs := make([]int64, 0, len(docs))
	chunkIndexes := make([]int64, 0, len(docs))
	chunkTexts := make([]string, 0, len(docs))
	vectors := make([][]float32, 0, len(docs))

	for _, doc := range docs {
		if len(doc.Vector) != cfg.Dimension {
			return fmt.Errorf("invalid vector dim=%d, expected=%d", len(doc.Vector), cfg.Dimension)
		}

		chunkIDs = append(chunkIDs, buildChunkID(doc.PostID, doc.ChunkIndex))
		postIDs = append(postIDs, doc.PostID)
		chunkIndexes = append(chunkIndexes, doc.ChunkIndex)
		chunkTexts = append(chunkTexts, doc.ChunkText)
		vectors = append(vectors, doc.Vector)
	}

	_, err := cli.Upsert(
		ctx,
		cfg.Collection,
		"",
		entity.NewColumnVarChar(fieldChunkID, chunkIDs),
		entity.NewColumnInt64(fieldPostID, postIDs),
		entity.NewColumnInt64(fieldChunkIdx, chunkIndexes),
		entity.NewColumnVarChar(fieldChunkText, chunkTexts),
		entity.NewColumnFloatVector(fieldEmbedding, cfg.Dimension, vectors),
	)
	return err
}

// SearchByVector 在 Milvus 中召回 chunk 级结果，并把结果字段一起带回业务层。
func SearchByVector(ctx context.Context, vector []float32, topK int) ([]SearchHit, error) {
	if !Enabled() {
		return nil, errors.New("milvus is disabled")
	}
	if len(vector) != cfg.Dimension {
		return nil, fmt.Errorf("invalid vector dim=%d, expected=%d", len(vector), cfg.Dimension)
	}
	if topK <= 0 {
		topK = cfg.TopK
	}
	if topK <= 0 {
		topK = 5
	}

	searchLimit := topK * 4 // chunk 检索需要多取一些候选，避免同一帖子多个 chunk 占满 TopK。
	if searchLimit < 10 {
		searchLimit = 10
	}

	searchParam, err := buildSearchParam()
	if err != nil {
		return nil, err
	}

	results, err := cli.Search(
		ctx,
		cfg.Collection,
		nil,
		"",
		[]string{fieldPostID, fieldChunkIdx, fieldChunkText},
		[]entity.Vector{entity.FloatVector(vector)},
		fieldEmbedding,
		parseMetricType(cfg.MetricType),
		searchLimit,
		searchParam,
	)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 || results[0].ResultCount == 0 {
		return nil, nil
	}

	postIDColumn := results[0].Fields.GetColumn(fieldPostID)
	chunkIndexColumn := results[0].Fields.GetColumn(fieldChunkIdx)
	chunkTextColumn := results[0].Fields.GetColumn(fieldChunkText)
	if postIDColumn == nil || chunkIndexColumn == nil || chunkTextColumn == nil {
		return nil, errors.New("milvus search result missing required fields")
	}

	hits := make([]SearchHit, 0, results[0].ResultCount)
	for i := 0; i < results[0].ResultCount; i++ {
		chunkID, err := results[0].IDs.GetAsString(i)
		if err != nil {
			return nil, err
		}
		postID, err := postIDColumn.GetAsInt64(i)
		if err != nil {
			return nil, err
		}
		chunkIndex, err := chunkIndexColumn.GetAsInt64(i)
		if err != nil {
			return nil, err
		}
		chunkText, err := chunkTextColumn.GetAsString(i)
		if err != nil {
			return nil, err
		}

		hits = append(hits, SearchHit{
			ChunkID:    chunkID,
			PostID:     postID,
			ChunkIndex: chunkIndex,
			ChunkText:  chunkText,
			Score:      results[0].Scores[i],
		})
	}

	return deduplicateHitsByPost(hits, topK), nil
}

// ensureCollection 确保 collection 存在；chunk 化后如果沿用旧集合名，会直接报 schema 不匹配，因此建议换新集合名。
func ensureCollection(ctx context.Context) error {
	has, err := cli.HasCollection(ctx, cfg.Collection)
	if err != nil {
		return err
	}
	if has {
		return validateCollectionSchema(ctx)
	}

	schema := entity.NewSchema().
		WithName(cfg.Collection).
		WithDescription("bluebell rag chunks").
		WithAutoID(false).
		WithField(entity.NewField().WithName(fieldChunkID).WithDataType(entity.FieldTypeVarChar).WithMaxLength(128).WithIsPrimaryKey(true).WithIsAutoID(false)).
		WithField(entity.NewField().WithName(fieldPostID).WithDataType(entity.FieldTypeInt64)).
		WithField(entity.NewField().WithName(fieldChunkIdx).WithDataType(entity.FieldTypeInt64)).
		WithField(entity.NewField().WithName(fieldChunkText).WithDataType(entity.FieldTypeVarChar).WithMaxLength(65535)).
		WithField(entity.NewField().WithName(fieldEmbedding).WithDataType(entity.FieldTypeFloatVector).WithDim(int64(cfg.Dimension)))

	if err := cli.CreateCollection(ctx, schema, entity.DefaultShardNumber); err != nil {
		return err
	}

	var idx entity.Index
	switch strings.ToUpper(cfg.IndexType) {
	case "IVF_FLAT":
		idx, err = entity.NewIndexIvfFlat(parseMetricType(cfg.MetricType), 1024)
	default:
		idx, err = entity.NewIndexHNSW(parseMetricType(cfg.MetricType), defaultHNSWM(), defaultHNSWEFConstruction())
	}
	if err != nil {
		return err
	}

	return cli.CreateIndex(ctx, cfg.Collection, fieldEmbedding, idx, false)
}

func deletePostChunks(ctx context.Context, postID int64) error {
	expr := fmt.Sprintf("%s == %d", fieldPostID, postID)
	return cli.Delete(ctx, cfg.Collection, "", expr)
}

func deduplicateHitsByPost(hits []SearchHit, topK int) []SearchHit {
	if len(hits) == 0 {
		return nil
	}

	result := make([]SearchHit, 0, topK)
	seen := make(map[int64]struct{}, topK)
	for _, hit := range hits {
		if _, ok := seen[hit.PostID]; ok {
			continue
		}
		result = append(result, hit)
		seen[hit.PostID] = struct{}{}
		if topK > 0 && len(result) >= topK {
			break
		}
	}
	return result
}

func validateCollectionSchema(ctx context.Context) error {
	coll, err := cli.DescribeCollection(ctx, cfg.Collection)
	if err != nil {
		return err
	}
	if coll == nil || coll.Schema == nil {
		return errors.New("milvus collection schema is empty")
	}

	fieldSet := make(map[string]struct{}, len(coll.Schema.Fields))
	for _, field := range coll.Schema.Fields {
		fieldSet[field.Name] = struct{}{}
	}

	required := []string{fieldChunkID, fieldPostID, fieldChunkIdx, fieldChunkText, fieldEmbedding}
	for _, name := range required {
		if _, ok := fieldSet[name]; !ok {
			return fmt.Errorf("milvus collection %s schema is incompatible with chunk mode, missing field %s", cfg.Collection, name)
		}
	}
	return nil
}

func buildSearchParam() (entity.SearchParam, error) {
	switch strings.ToUpper(cfg.IndexType) {
	case "IVF_FLAT":
		return entity.NewIndexIvfFlatSearchParam(1024)
	default:
		return entity.NewIndexHNSWSearchParam(defaultSearchEF())
	}
}

func buildChunkID(postID int64, chunkIndex int64) string {
	return fmt.Sprintf("%d_%d", postID, chunkIndex) // 用字符串主键避免大 post_id 在拼接时产生 int64 溢出。
}

func parseMetricType(metric string) entity.MetricType {
	switch strings.ToUpper(metric) {
	case "IP":
		return entity.IP
	case "L2":
		return entity.L2
	default:
		return entity.COSINE
	}
}

func defaultHNSWM() int {
	if cfg.HNSWM > 0 {
		return cfg.HNSWM
	}
	return 16
}

func defaultHNSWEFConstruction() int {
	if cfg.HNSWEFConstruction > 0 {
		return cfg.HNSWEFConstruction
	}
	return 200
}

func defaultSearchEF() int {
	if cfg.SearchEf > 0 {
		return cfg.SearchEf
	}
	return 64
}
