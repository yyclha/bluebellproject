package milvus

import (
	"context"
	"errors"
	"fmt"
	"gamebase/internal/setting"
	"strings"
	"time"

	"github.com/milvus-io/milvus/client/v2/entity"
	"github.com/milvus-io/milvus/client/v2/index"
	"github.com/milvus-io/milvus/client/v2/milvusclient"
)

const (
	fieldChunkID         = "chunk_id"
	fieldPostID          = "post_id"
	fieldChunkIdx        = "chunk_index"
	fieldChunkText       = "chunk_text"
	fieldEmbedding       = "embedding"
	fieldSparseEmbedding = "sparse_embedding"

	bm25FunctionName = "chunk_text_bm25"
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
	cli    *milvusclient.Client
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
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		var err error
		cli, err = milvusclient.New(ctx, &milvusclient.ClientConfig{Address: cfg.Address})
		if err == nil {
			err = ensureCollection(ctx)
		}
		if err == nil {
			err = loadCollection(ctx)
		}
		cancel()
		if err == nil {
			loaded = true
			return nil
		}
		lastErr = err
		if cli != nil {
			_ = cli.Close(context.Background())
			cli = nil
		}
		time.Sleep(3 * time.Second)
	}
	return lastErr
}

// Close 关闭 Milvus 客户端连接。
func Close() {
	if cli != nil {
		_ = cli.Close(context.Background())
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

	_, err := cli.Upsert(ctx, milvusclient.NewColumnBasedInsertOption(cfg.Collection).
		WithVarcharColumn(fieldChunkID, chunkIDs).
		WithInt64Column(fieldPostID, postIDs).
		WithInt64Column(fieldChunkIdx, chunkIndexes).
		WithVarcharColumn(fieldChunkText, chunkTexts).
		WithFloatVectorColumn(fieldEmbedding, cfg.Dimension, vectors),
	)
	return err
}

// DeletePostChunks 在 Milvus 中删除指定帖子的分块记录。
func DeletePostChunks(ctx context.Context, postID int64) error {
	if !Enabled() {
		return nil
	}
	return deletePostChunks(ctx, postID)
}

// RebuildCollection drops and recreates the configured collection and index,
// then loads it back into memory.
func RebuildCollection(ctx context.Context) error {
	if cfg == nil || !cfg.Enabled {
		return errors.New("milvus is disabled")
	}
	if cli == nil {
		return errors.New("milvus client is not initialized")
	}

	loaded = false

	has, err := cli.HasCollection(ctx, milvusclient.NewHasCollectionOption(cfg.Collection))
	if err != nil {
		return err
	}
	if has {
		_ = cli.ReleaseCollection(ctx, milvusclient.NewReleaseCollectionOption(cfg.Collection))
		if err := cli.DropCollection(ctx, milvusclient.NewDropCollectionOption(cfg.Collection)); err != nil {
			return err
		}
	}

	if err := ensureCollection(ctx); err != nil {
		return err
	}
	if err := loadCollection(ctx); err != nil {
		return err
	}

	loaded = true
	return nil
}

// SearchByHybrid 在 Milvus 中执行 dense embedding + BM25 sparse hybrid search。
func SearchByHybrid(ctx context.Context, query string, vector []float32, topK int) ([]SearchHit, error) {
	if !Enabled() {
		return nil, errors.New("milvus is disabled")
	}
	if strings.TrimSpace(query) == "" {
		return nil, errors.New("query is empty")
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

	denseReq := milvusclient.NewAnnRequest(fieldEmbedding, searchLimit, entity.FloatVector(vector)).
		WithAnnParam(buildSearchParam())
	sparseReq := milvusclient.NewAnnRequest(fieldSparseEmbedding, searchLimit, entity.Text(strings.TrimSpace(query))).
		WithAnnParam(index.NewSparseAnnParam())

	results, err := cli.HybridSearch(ctx, milvusclient.NewHybridSearchOption(
		cfg.Collection,
		searchLimit,
		denseReq,
		sparseReq,
	).WithReranker(milvusclient.NewRRFReranker()).
		WithOutputFields(fieldPostID, fieldChunkIdx, fieldChunkText),
	)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 || results[0].ResultCount == 0 {
		return nil, nil
	}

	hits, err := parseHitsFromResultSet(results[0])
	if err != nil {
		return nil, err
	}

	return deduplicateHitsByPost(hits, topK), nil
}

// SearchByVector 保留兼容入口，内部使用同一向量同时执行 dense-only 检索。
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

	searchLimit := topK * 4
	if searchLimit < 10 {
		searchLimit = 10
	}

	results, err := cli.Search(ctx, milvusclient.NewSearchOption(
		cfg.Collection,
		searchLimit,
		[]entity.Vector{entity.FloatVector(vector)},
	).WithANNSField(fieldEmbedding).
		WithAnnParam(buildSearchParam()).
		WithOutputFields(fieldPostID, fieldChunkIdx, fieldChunkText),
	)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 || results[0].ResultCount == 0 {
		return nil, nil
	}

	hits, err := parseHitsFromResultSet(results[0])
	if err != nil {
		return nil, err
	}

	return deduplicateHitsByPost(hits, topK), nil
}

// ensureCollection 确保 collection 存在；hybrid schema 不能复用旧 dense-only collection。
// SearchBySparseText runs BM25 sparse search only. It is used by diagnostics
// to separate sparse retrieval problems from dense/hybrid retrieval problems.
func SearchBySparseText(ctx context.Context, query string, topK int) ([]SearchHit, error) {
	if !Enabled() {
		return nil, errors.New("milvus is disabled")
	}
	if strings.TrimSpace(query) == "" {
		return nil, errors.New("query is empty")
	}
	if topK <= 0 {
		topK = cfg.TopK
	}
	if topK <= 0 {
		topK = 5
	}

	results, err := cli.Search(ctx, milvusclient.NewSearchOption(
		cfg.Collection,
		topK,
		[]entity.Vector{entity.Text(strings.TrimSpace(query))},
	).WithANNSField(fieldSparseEmbedding).
		WithAnnParam(index.NewSparseAnnParam()).
		WithOutputFields(fieldPostID, fieldChunkIdx, fieldChunkText),
	)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 || results[0].ResultCount == 0 {
		return nil, nil
	}

	hits, err := parseHitsFromResultSet(results[0])
	if err != nil {
		return nil, err
	}

	return deduplicateHitsByPost(hits, topK), nil
}

// CollectionStats returns Milvus collection stats for diagnostics.
func CollectionStats(ctx context.Context) (map[string]string, error) {
	if !Enabled() {
		return nil, errors.New("milvus is disabled")
	}
	return cli.GetCollectionStats(ctx, milvusclient.NewGetCollectionStatsOption(cfg.Collection))
}

// SampleChunks returns a few arbitrary chunks from the collection for diagnostics.
func SampleChunks(ctx context.Context, limit int) ([]SearchHit, error) {
	if !Enabled() {
		return nil, errors.New("milvus is disabled")
	}
	if limit <= 0 {
		limit = 3
	}

	result, err := cli.Query(ctx, milvusclient.NewQueryOption(cfg.Collection).
		WithFilter(fieldPostID+" >= 0").
		WithLimit(limit).
		WithOutputFields(fieldChunkID, fieldPostID, fieldChunkIdx, fieldChunkText),
	)
	if err != nil {
		return nil, err
	}
	if result.ResultCount == 0 {
		return nil, nil
	}

	return parseHitsFromResultSet(result)
}

func parseHitsFromResultSet(result milvusclient.ResultSet) ([]SearchHit, error) {
	if result.ResultCount == 0 {
		return nil, nil
	}

	postIDColumn := result.GetColumn(fieldPostID)
	chunkIndexColumn := result.GetColumn(fieldChunkIdx)
	chunkTextColumn := result.GetColumn(fieldChunkText)
	if postIDColumn == nil || chunkIndexColumn == nil || chunkTextColumn == nil {
		return nil, errors.New("milvus result missing required fields")
	}

	chunkIDColumn := result.GetColumn(fieldChunkID)
	hits := make([]SearchHit, 0, result.ResultCount)
	for i := 0; i < result.ResultCount; i++ {
		chunkID := ""
		var err error
		if chunkIDColumn != nil {
			chunkID, err = chunkIDColumn.GetAsString(i)
		} else if result.IDs != nil {
			chunkID, err = result.IDs.GetAsString(i)
		}
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

		hit := SearchHit{
			ChunkID:    chunkID,
			PostID:     postID,
			ChunkIndex: chunkIndex,
			ChunkText:  chunkText,
		}
		if i < len(result.Scores) {
			hit.Score = result.Scores[i]
		}
		hits = append(hits, hit)
	}
	return hits, nil
}

func ensureCollection(ctx context.Context) error {
	has, err := cli.HasCollection(ctx, milvusclient.NewHasCollectionOption(cfg.Collection))
	if err != nil {
		return err
	}
	if has {
		return validateCollectionSchema(ctx)
	}

	schema := entity.NewSchema().
		WithName(cfg.Collection).
		WithDescription("gamebase rag chunks with dense and bm25 sparse retrieval").
		WithAutoID(false).
		WithField(entity.NewField().WithName(fieldChunkID).WithDataType(entity.FieldTypeVarChar).WithMaxLength(128).WithIsPrimaryKey(true).WithIsAutoID(false)).
		WithField(entity.NewField().WithName(fieldPostID).WithDataType(entity.FieldTypeInt64)).
		WithField(entity.NewField().WithName(fieldChunkIdx).WithDataType(entity.FieldTypeInt64)).
		WithField(entity.NewField().WithName(fieldChunkText).WithDataType(entity.FieldTypeVarChar).WithMaxLength(65535).WithEnableAnalyzer(true)).
		WithField(entity.NewField().WithName(fieldEmbedding).WithDataType(entity.FieldTypeFloatVector).WithDim(int64(cfg.Dimension))).
		WithField(entity.NewField().WithName(fieldSparseEmbedding).WithDataType(entity.FieldTypeSparseVector)).
		WithFunction(entity.NewFunction().
			WithName(bm25FunctionName).
			WithType(entity.FunctionTypeBM25).
			WithInputFields(fieldChunkText).
			WithOutputFields(fieldSparseEmbedding),
		)

	indexOptions := []milvusclient.CreateIndexOption{
		milvusclient.NewCreateIndexOption(cfg.Collection, fieldEmbedding, buildDenseIndex()).WithIndexName(fieldEmbedding),
		milvusclient.NewCreateIndexOption(cfg.Collection, fieldSparseEmbedding, index.NewSparseInvertedIndex(entity.BM25, 0)).WithIndexName(fieldSparseEmbedding),
	}

	return cli.CreateCollection(ctx, milvusclient.NewCreateCollectionOption(cfg.Collection, schema).WithIndexOptions(indexOptions...))
}

func loadCollection(ctx context.Context) error {
	task, err := cli.LoadCollection(ctx, milvusclient.NewLoadCollectionOption(cfg.Collection))
	if err != nil {
		return err
	}
	return task.Await(ctx)
}

// deletePostChunks 在 Milvus 中删除指定帖子的分块记录。
func deletePostChunks(ctx context.Context, postID int64) error {
	expr := fmt.Sprintf("%s == %d", fieldPostID, postID)
	_, err := cli.Delete(ctx, milvusclient.NewDeleteOption(cfg.Collection).WithExpr(expr))
	return err
}

// deduplicateHitsByPost 按帖子维度对检索结果去重。
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

// validateCollectionSchema 校验 Milvus 集合结构是否符合 hybrid BM25 schema。
func validateCollectionSchema(ctx context.Context) error {
	coll, err := cli.DescribeCollection(ctx, milvusclient.NewDescribeCollectionOption(cfg.Collection))
	if err != nil {
		return err
	}
	if coll == nil || coll.Schema == nil {
		return errors.New("milvus collection schema is empty")
	}

	fieldSet := make(map[string]entity.FieldType, len(coll.Schema.Fields))
	for _, field := range coll.Schema.Fields {
		fieldSet[field.Name] = field.DataType
	}

	required := map[string]entity.FieldType{
		fieldChunkID:         entity.FieldTypeVarChar,
		fieldPostID:          entity.FieldTypeInt64,
		fieldChunkIdx:        entity.FieldTypeInt64,
		fieldChunkText:       entity.FieldTypeVarChar,
		fieldEmbedding:       entity.FieldTypeFloatVector,
		fieldSparseEmbedding: entity.FieldTypeSparseVector,
	}
	for name, wantType := range required {
		gotType, ok := fieldSet[name]
		if !ok {
			return fmt.Errorf("milvus collection %s schema is incompatible with hybrid RAG mode, missing field %s; use a new collection name or rebuild collection", cfg.Collection, name)
		}
		if gotType != wantType {
			return fmt.Errorf("milvus collection %s field %s type=%s, expected=%s", cfg.Collection, name, gotType.Name(), wantType.Name())
		}
	}
	return nil
}

func buildDenseIndex() index.Index {
	switch strings.ToUpper(cfg.IndexType) {
	case "IVF_FLAT":
		return index.NewIvfFlatIndex(parseMetricType(cfg.MetricType), 1024)
	default:
		return index.NewHNSWIndex(parseMetricType(cfg.MetricType), defaultHNSWM(), defaultHNSWEFConstruction())
	}
}

// buildSearchParam 构建 Milvus dense 检索参数。
func buildSearchParam() index.AnnParam {
	switch strings.ToUpper(cfg.IndexType) {
	case "IVF_FLAT":
		return index.NewIvfAnnParam(1024)
	default:
		return index.NewHNSWAnnParam(defaultSearchEF())
	}
}

// buildChunkID 根据帖子和分块序号生成唯一分块 ID。
func buildChunkID(postID int64, chunkIndex int64) string {
	return fmt.Sprintf("%d_%d", postID, chunkIndex) // 用字符串主键避免大 post_id 在拼接时产生 int64 溢出。
}

// parseMetricType 将配置中的度量类型转换为 Milvus 枚举。
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

// defaultHNSWM 返回 HNSW 索引的默认 M 参数。
func defaultHNSWM() int {
	if cfg.HNSWM > 0 {
		return cfg.HNSWM
	}
	return 16
}

// defaultHNSWEFConstruction 返回 HNSW 索引的默认构建 ef 参数。
func defaultHNSWEFConstruction() int {
	if cfg.HNSWEFConstruction > 0 {
		return cfg.HNSWEFConstruction
	}
	return 200
}

// defaultSearchEF 返回 HNSW 检索的默认 ef 参数。
func defaultSearchEF() int {
	if cfg.SearchEf > 0 {
		return cfg.SearchEf
	}
	return 64
}
