package controller

import (
	"bluebell/logic"
	"bluebell/models"
	"bluebell/setting"
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

func RAGChatHandler(c *gin.Context) {
	p := new(models.ParamRAGChat)
	if err := c.ShouldBindQuery(p); err != nil {
		if err := c.ShouldBindJSON(p); err != nil {
			ResponseError(c, CodeInvalidParam)
			return
		}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), ragChatRequestTimeout())
	defer cancel()
	data, err := logic.AskRAGAssistant(ctx, p)
	if err != nil {
		zap.L().Error("AskRAGAssistant failed", zap.Error(err))
		ResponseErrorWithMsg(c, CodeServerBusy, err.Error())
		return
	}
	ResponseSuccess(c, data)
}

func RAGChatStreamHandler(c *gin.Context) {
	p := new(models.ParamRAGChat)
	if err := c.ShouldBindQuery(p); err != nil {
		if err := c.ShouldBindJSON(p); err != nil {
			ResponseError(c, CodeInvalidParam)
			return
		}
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), ragChatRequestTimeout())
	defer cancel()

	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	writer := c.Writer

	writeEvent := func(event string, payload interface{}) error {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(writer, "event: %s\n", event); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(writer, "data: %s\n\n", raw); err != nil {
			return err
		}
		writer.Flush()
		return nil
	}

	if err := writeEvent("start", gin.H{"question": p.Question}); err != nil {
		return
	}

	data, err := logic.StreamRAGAssistant(ctx, p, func(chunk string) error {
		if strings.TrimSpace(chunk) == "" {
			return nil
		}
		return writeEvent("chunk", gin.H{"delta": chunk})
	})
	if err != nil {
		zap.L().Error("StreamRAGAssistant failed", zap.Error(err))
		_ = writeEvent("error", gin.H{"message": err.Error()})
		return
	}

	_ = writeEvent("done", gin.H{
		"question": data.Question,
		"answer":   data.Answer,
		"model":    data.Model,
		"hits":     data.Hits,
	})
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

func ragChatRequestTimeout() time.Duration {
	timeoutSeconds := 60
	if setting.Conf != nil && setting.Conf.RAGChatConfig != nil && setting.Conf.RAGChatConfig.TimeoutSeconds > 0 {
		timeoutSeconds = setting.Conf.RAGChatConfig.TimeoutSeconds + 20
	}
	if timeoutSeconds < 60 {
		timeoutSeconds = 60
	}
	return time.Duration(timeoutSeconds) * time.Second
}
