package controller

import "bluebell/internal/models"

// _ResponsePostList 专门用于 Swagger 文档中的帖子列表响应模型。
type _ResponsePostList struct {
	Code    ResCode                 `json:"code"`    // 业务状态码
	Message string                  `json:"message"` // 提示信息
	Data    []*models.ApiPostDetail `json:"data"`    // 响应数据
}
