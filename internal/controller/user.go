package controller

import (
	"gamebase/internal/dao/mysql"
	"gamebase/internal/logic"
	"gamebase/internal/models"
	"errors"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"go.uber.org/zap"
)

func SignUpHandler(c *gin.Context) {
	p := new(models.ParamSignUp)
	if err := c.ShouldBindJSON(p); err != nil {
		zap.L().Error("SignUp with invalid param", zap.Error(err))
		errs, ok := err.(validator.ValidationErrors)
		if !ok {
			ResponseError(c, CodeInvalidParam)
			return
		}
		ResponseErrorWithMsg(c, CodeInvalidParam, removeTopStruct(errs.Translate(trans)))
		return
	}

	if err := logic.SignUp(p); err != nil {
		zap.L().Error("logic.SignUp failed", zap.Error(err))
		if errors.Is(err, mysql.ErrorUserExist) {
			ResponseError(c, CodeUserExist)
			return
		}
		ResponseError(c, CodeServerBusy)
		return
	}

	ResponseSuccess(c, nil)
}

func LoginHandler(c *gin.Context) {
	p := new(models.ParamLogin)
	if err := c.ShouldBindJSON(p); err != nil {
		zap.L().Error("Login with invalid param", zap.Error(err))
		errs, ok := err.(validator.ValidationErrors)
		if !ok {
			ResponseError(c, CodeInvalidParam)
			return
		}
		ResponseErrorWithMsg(c, CodeInvalidParam, removeTopStruct(errs.Translate(trans)))
		return
	}

	user, err := logic.Login(p)
	if err != nil {
		zap.L().Error("logic.Login failed", zap.String("username", p.Username), zap.Error(err))
		if errors.Is(err, mysql.ErrorUserNotExist) {
			ResponseError(c, CodeUserNotExist)
			return
		}
		ResponseError(c, CodeInvalidPassword)
		return
	}

	ResponseSuccess(c, gin.H{
		"user_id":    fmt.Sprintf("%d", user.UserID),
		"user_name":  user.Username,
		"token":      user.Token,
		"email":      user.Email,
		"gender":     user.Gender,
		"avatar_url": user.AvatarURL,
		"bio":        user.Bio,
	})
}

func GetCurrentUserHandler(c *gin.Context) {
	userID, err := getCurrentUserID(c)
	if err != nil {
		ResponseError(c, CodeNeedLogin)
		return
	}
	user, err := mysql.GetUserById(userID)
	if err != nil {
		zap.L().Error("mysql.GetUserById failed", zap.Int64("user_id", userID), zap.Error(err))
		ResponseError(c, CodeServerBusy)
		return
	}
	ResponseSuccess(c, user)
}

func UpdateCurrentUserProfileHandler(c *gin.Context) {
	userID, err := getCurrentUserID(c)
	if err != nil {
		ResponseError(c, CodeNeedLogin)
		return
	}
	p := new(models.ParamUpdateProfile)
	if err := c.ShouldBindJSON(p); err != nil {
		zap.L().Error("UpdateCurrentUserProfile with invalid param", zap.Error(err))
		errs, ok := err.(validator.ValidationErrors)
		if !ok {
			ResponseError(c, CodeInvalidParam)
			return
		}
		ResponseErrorWithMsg(c, CodeInvalidParam, removeTopStruct(errs.Translate(trans)))
		return
	}
	if err := mysql.CheckUserExistExceptUserID(p.Username, userID); err != nil {
		if errors.Is(err, mysql.ErrorUserExist) {
			ResponseError(c, CodeUserExist)
			return
		}
		zap.L().Error("mysql.CheckUserExistExceptUserID failed", zap.Int64("user_id", userID), zap.Error(err))
		ResponseError(c, CodeServerBusy)
		return
	}
	if err := mysql.UpdateUserProfile(userID, p); err != nil {
		zap.L().Error("mysql.UpdateUserProfile failed", zap.Int64("user_id", userID), zap.Error(err))
		ResponseError(c, CodeServerBusy)
		return
	}
	user, err := mysql.GetUserById(userID)
	if err != nil {
		ResponseError(c, CodeServerBusy)
		return
	}
	ResponseSuccess(c, user)
}

func ChangeCurrentUserPasswordHandler(c *gin.Context) {
	userID, err := getCurrentUserID(c)
	if err != nil {
		ResponseError(c, CodeNeedLogin)
		return
	}
	p := new(models.ParamChangePassword)
	if err := c.ShouldBindJSON(p); err != nil {
		zap.L().Error("ChangeCurrentUserPassword with invalid param", zap.Error(err))
		errs, ok := err.(validator.ValidationErrors)
		if !ok {
			ResponseError(c, CodeInvalidParam)
			return
		}
		ResponseErrorWithMsg(c, CodeInvalidParam, removeTopStruct(errs.Translate(trans)))
		return
	}
	if err := mysql.CheckUserPassword(userID, p.OldPassword); err != nil {
		if errors.Is(err, mysql.ErrorInvalidPassword) {
			ResponseError(c, CodeInvalidPassword)
			return
		}
		zap.L().Error("mysql.CheckUserPassword failed", zap.Int64("user_id", userID), zap.Error(err))
		ResponseError(c, CodeServerBusy)
		return
	}
	if err := mysql.UpdateUserPassword(userID, p.NewPassword); err != nil {
		zap.L().Error("mysql.UpdateUserPassword failed", zap.Int64("user_id", userID), zap.Error(err))
		ResponseError(c, CodeServerBusy)
		return
	}
	ResponseSuccess(c, gin.H{"changed": true})
}
