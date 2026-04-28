package logic

import (
	"bluebell/internal/dao/mysql"
	"bluebell/internal/models"
)

// GetCommunityList 获取社区列表数据。
func GetCommunityList() ([]*models.Community, error) {
	// 查数据库 查找到所有的community 并返回
	return mysql.GetCommunityList()
}

// GetCommunityDetail 获取指定社区的详情信息。
func GetCommunityDetail(id int64) (*models.CommunityDetail, error) {
	return mysql.GetCommunityDetailByID(id)
}
