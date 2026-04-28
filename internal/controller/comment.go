package controller

import (
	"bluebell/internal/logic"
	"bluebell/internal/models"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// GetCommentListHandler 处理获取评论列表请求。
func GetCommentListHandler(c *gin.Context) {
	pidStr := c.Param("id")
	pid, err := strconv.ParseInt(pidStr, 10, 64)
	if err != nil {
		ResponseError(c, CodeInvalidParam)
		return
	}

	data, err := logic.GetCommentListByPostID(pid)
	if err != nil {
		zap.L().Error("logic.GetCommentListByPostID failed", zap.Int64("post_id", pid), zap.Error(err))
		ResponseError(c, CodeServerBusy)
		return
	}
	ResponseSuccess(c, data)
}

// CreateCommentHandler 处理创建评论请求。
func CreateCommentHandler(c *gin.Context) {
	p := new(models.ParamCreateComment)
	if err := c.ShouldBindJSON(p); err != nil {
		ResponseError(c, CodeInvalidParam)
		return
	}

	userID, err := getCurrentUserID(c)
	if err != nil {
		ResponseError(c, CodeNeedLogin)
		return
	}

	if err := logic.CreateComment(p, userID); err != nil {
		zap.L().Error("logic.CreateComment failed", zap.Int64("post_id", p.PostID), zap.Int64("author_id", userID), zap.Error(err))
		ResponseError(c, CodeServerBusy)
		return
	}
	ResponseSuccess(c, nil)
}
