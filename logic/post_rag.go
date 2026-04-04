package logic

import (
	"bluebell/models"
	"context"
	"time"

	"go.uber.org/zap"
)

func CreatePostWithRAG(p *models.Post) error {
	if err := CreatePost(p); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := IndexPostToRAG(ctx, p); err != nil {
		zap.L().Warn("IndexPostToRAG failed", zap.Int64("post_id", p.ID), zap.Error(err))
	}
	return nil
}
