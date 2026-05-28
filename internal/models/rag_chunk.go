package models

import "time"

// PostRAGChunk 表示帖子切块的持久化记录，用于构建稀疏检索特征与索引重建。
type PostRAGChunk struct {
	ID         int64     `db:"id"`
	PostID     int64     `db:"post_id"`
	ChunkIndex int64     `db:"chunk_index"`
	ChunkText  string    `db:"chunk_text"`
	CreateTime time.Time `db:"create_time"`
	UpdateTime time.Time `db:"update_time"`
}
