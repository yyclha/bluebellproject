package controller

import (
	"bluebell/internal/logic"
	"errors"

	"github.com/gin-gonic/gin"
)

// UploadImageHandler 处理帖子图片上传。
func UploadImageHandler(c *gin.Context) {
	file, header, err := c.Request.FormFile("image")
	if err != nil {
		ResponseErrorWithMsg(c, CodeInvalidParam, "请选择要上传的图片")
		return
	}
	defer file.Close()

	image, err := logic.UploadImageToCOS(c.Request.Context(), file, header)
	if err != nil {
		switch {
		case errors.Is(err, logic.ErrCOSDisabled):
			ResponseErrorWithMsg(c, CodeServerBusy, "图片上传未启用")
		case errors.Is(err, logic.ErrCOSConfigInvalid):
			ResponseErrorWithMsg(c, CodeServerBusy, "图片上传配置不完整")
		case errors.Is(err, logic.ErrImageTooLarge):
			ResponseErrorWithMsg(c, CodeInvalidParam, "图片不能超过上传大小限制")
		case errors.Is(err, logic.ErrImageTypeInvalid):
			ResponseErrorWithMsg(c, CodeInvalidParam, "仅支持 jpg、png、webp、gif 图片")
		default:
			ResponseErrorWithMsg(c, CodeServerBusy, "图片上传失败")
		}
		return
	}

	ResponseSuccess(c, image)
}
