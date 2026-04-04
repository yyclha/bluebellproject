package controller

import (
	"errors"

	redisdao "bluebell/dao/redis"
	"bluebell/logic"
	"bluebell/models"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"go.uber.org/zap"
)

func PostVoteController(c *gin.Context) {
	p := new(models.ParamVoteData)
	if err := c.ShouldBindJSON(p); err != nil {
		errs, ok := err.(validator.ValidationErrors)
		if !ok {
			ResponseError(c, CodeInvalidParam)
			return
		}
		errData := removeTopStruct(errs.Translate(trans))
		ResponseErrorWithMsg(c, CodeInvalidParam, errData)
		return
	}

	userID, err := getCurrentUserID(c)
	if err != nil {
		ResponseError(c, CodeNeedLogin)
		return
	}

	if err := logic.VoteForPost(userID, p); err != nil {
		zap.L().Error("logic.VoteForPost() failed", zap.Error(err))
		switch {
		case errors.Is(err, redisdao.ErrVoteTimeExpire):
			ResponseErrorWithMsg(c, CodeServerBusy, "帖子投票时间已过")
		case errors.Is(err, redisdao.ErrVoteRepeated):
			ResponseErrorWithMsg(c, CodeServerBusy, "请不要重复投票")
		default:
			ResponseErrorWithMsg(c, CodeServerBusy, "投票失败，请稍后重试")
		}
		return
	}

	ResponseSuccess(c, nil)
}
