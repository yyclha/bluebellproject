package mysql

import (
	"gamebase/internal/models"
	"strings"

	"github.com/jmoiron/sqlx"
)

// InitPostRAGChunkTable 初始化帖子 RAG 切块表。
func InitPostRAGChunkTable() error {
	sqlStr := `
CREATE TABLE IF NOT EXISTS post_rag_chunk (
	id BIGINT NOT NULL AUTO_INCREMENT,
	post_id BIGINT NOT NULL,
	chunk_index BIGINT NOT NULL,
	chunk_text TEXT NOT NULL,
	create_time TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP,
	update_time TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
	PRIMARY KEY (id),
	UNIQUE KEY idx_post_chunk (post_id, chunk_index),
	KEY idx_post_id (post_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
`
	_, err := db.Exec(sqlStr)
	return err
}

// ReplacePostRAGChunks 全量替换指定帖子的切块文本。
func ReplacePostRAGChunks(postID int64, chunks []models.PostRAGChunk) (err error) {
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.Exec(`delete from post_rag_chunk where post_id = ?`, postID); err != nil {
		return err
	}
	if len(chunks) == 0 {
		return tx.Commit()
	}

	sqlStr := `insert into post_rag_chunk (post_id, chunk_index, chunk_text) values (?, ?, ?)`
	for _, chunk := range chunks {
		if _, err = tx.Exec(sqlStr, chunk.PostID, chunk.ChunkIndex, chunk.ChunkText); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeletePostRAGChunks 删除指定帖子的切块文本。
func DeletePostRAGChunks(postID int64) error {
	_, err := db.Exec(`delete from post_rag_chunk where post_id = ?`, postID)
	return err
}

// GetPostRAGChunksByPostIDs 根据帖子 ID 列表查询切块数据。
func GetPostRAGChunksByPostIDs(postIDs []string) ([]*models.PostRAGChunk, error) {
	if len(postIDs) == 0 {
		return []*models.PostRAGChunk{}, nil
	}

	sqlStr := `select id, post_id, chunk_index, chunk_text, create_time, update_time
	from post_rag_chunk
	where post_id in (?)
	order by find_in_set(post_id, ?), chunk_index asc`

	query, args, err := sqlx.In(sqlStr, postIDs, strings.Join(postIDs, ","))
	if err != nil {
		return nil, err
	}
	query = db.Rebind(query)

	result := make([]*models.PostRAGChunk, 0, len(postIDs))
	if err := db.Select(&result, query, args...); err != nil {
		return nil, err
	}
	return result, nil
}
