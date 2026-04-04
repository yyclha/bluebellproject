package controller

import (
	"bluebell/logic"
	"bluebell/models"
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func RAGSearchHandler(c *gin.Context) {
	p := new(models.ParamRAGSearch)
	if err := c.ShouldBindQuery(p); err != nil {
		ResponseError(c, CodeInvalidParam)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	data, err := logic.SearchPostByRAG(ctx, p)
	if err != nil {
		zap.L().Error("SearchPostByRAG failed", zap.Error(err))
		ResponseError(c, CodeServerBusy)
		return
	}
	ResponseSuccess(c, data)
}

func RAGReindexHandler(c *gin.Context) {
	p := new(models.ParamRAGReindex)
	_ = c.ShouldBindJSON(p)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Minute)
	defer cancel()
	n, err := logic.ReindexPostToRAG(ctx, p.Limit)
	if err != nil {
		zap.L().Error("ReindexPostToRAG failed", zap.Error(err))
		ResponseError(c, CodeServerBusy)
		return
	}
	ResponseSuccess(c, gin.H{"indexed": n})
}
