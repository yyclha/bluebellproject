package logic

import (
	"bluebell/internal/dao/mysql"
	"bluebell/internal/dao/redis"
	"bluebell/internal/models"
	"bluebell/pkg/postscore"
	"context"
	"math"
	"time"

	"go.uber.org/zap"
)

// CreatePostWithRAG 创建帖子并触发 RAG 与 AI 评分异步任务。
func CreatePostWithRAG(p *models.Post) error {
	if err := CreatePost(p); err != nil {
		return err
	}

	postCopy := *p
	go runPostAsyncTasks(&postCopy)
	return nil
}

// runPostAsyncTasks 异步执行帖子创建后的扩展任务。
func runPostAsyncTasks(p *models.Post) {
	scoreCtx, scoreCancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer scoreCancel()
	if err := applyAISeedScore(scoreCtx, p); err != nil {
		zap.L().Warn("applyAISeedScore failed", zap.Int64("post_id", p.ID), zap.Error(err))
	}

	indexCtx, indexCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer indexCancel()
	if err := IndexPostToRAG(indexCtx, p); err != nil {
		zap.L().Warn("IndexPostToRAG failed", zap.Int64("post_id", p.ID), zap.Error(err))
	}
}

// applyAISeedScore 为帖子计算并写入初始 AI 分数。
func applyAISeedScore(ctx context.Context, p *models.Post) error {
	if !postscore.Enabled() {
		return nil
	}

	result, err := postscore.ScorePost(ctx, p)
	if err != nil {
		return err
	}

	// Keep the model score additive but bounded; 0-100 becomes a configurable
	// seed boost in the same Redis zset that drives score ordering.
	weight := postscore.ScoreWeight()
	delta := math.Round(result.Score * weight)
	if delta <= 0 {
		return nil
	}

	if err := redis.AddPostScore(p.ID, delta); err != nil {
		return err
	}
	if err := mysql.UpsertPostAIScore(&models.PostAIScore{
		PostID:       p.ID,
		Model:        result.Model,
		RawScore:     result.Score,
		ScoreWeight:  weight,
		ScoreDelta:   delta,
		ResponseText: result.ResponseText,
	}); err != nil {
		zap.L().Warn("UpsertPostAIScore failed", zap.Int64("post_id", p.ID), zap.Error(err))
	}

	zap.L().Info("post ai score applied",
		zap.Int64("post_id", p.ID),
		zap.Float64("ai_score", result.Score),
		zap.Float64("score_weight", weight),
		zap.Float64("score_delta", delta),
	)
	return nil
}
