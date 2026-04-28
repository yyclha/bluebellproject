package mysql

import (
	"bluebell/internal/models"
	"database/sql"

	"go.uber.org/zap"
)

// GetCommunityList 获取社区列表数据。
func GetCommunityList() (communityList []*models.Community, err error) {
	sqlStr := `select community_id, community_name, introduction
	from community
	order by case community_name
		when 'LOL' then 1
		when 'CF' then 2
		when '力扣' then 3
		else 99
	end, community_id`
	if err := db.Select(&communityList, sqlStr); err != nil {
		if err == sql.ErrNoRows {
			zap.L().Warn("there is no community in db")
			err = nil
		}
	}
	return
}

// GetCommunityDetailByID 根据ID查询社区详情
func GetCommunityDetailByID(id int64) (community *models.CommunityDetail, err error) {
	community = new(models.CommunityDetail)
	sqlStr := `select 
			community_id, community_name, introduction, create_time
			from community 
			where community_id = ?
	`
	if err := db.Get(community, sqlStr, id); err != nil {
		if err == sql.ErrNoRows {
			err = ErrorInvalidID
		}
	}
	return community, err
}

type defaultCommunity struct {
	Name         string
	Introduction string
	Aliases      []string
}

// InitDefaultCommunities 确保首页领域筛选需要的默认社区存在。
func InitDefaultCommunities() error {
	defaults := []defaultCommunity{
		{
			Name:         "LOL",
			Introduction: "英雄联盟开黑、版本理解、英雄攻略与赛事讨论",
			Aliases:      []string{"英雄联盟"},
		},
		{
			Name:         "CF",
			Introduction: "穿越火线枪法、地图点位、战术配合与活动交流",
			Aliases:      []string{"CS:GO", "CSGO", "穿越火线"},
		},
		{
			Name:         "力扣",
			Introduction: "算法刷题、面试准备、题解复盘与学习路线",
			Aliases:      []string{"leetcode", "LeetCode", "LC"},
		},
	}

	for _, item := range defaults {
		if err := normalizeDefaultCommunity(item); err != nil {
			return err
		}
		if err := ensureDefaultCommunity(item); err != nil {
			return err
		}
	}
	return nil
}

func normalizeDefaultCommunity(item defaultCommunity) error {
	for _, alias := range item.Aliases {
		var targetCount int
		if err := db.Get(&targetCount, "select count(1) from community where community_name = ?", item.Name); err != nil {
			return err
		}
		if targetCount > 0 {
			continue
		}
		if _, err := db.Exec(
			"update community set community_name = ?, introduction = ? where community_name = ?",
			item.Name,
			item.Introduction,
			alias,
		); err != nil {
			return err
		}
	}
	return nil
}

func ensureDefaultCommunity(item defaultCommunity) error {
	var count int
	if err := db.Get(&count, "select count(1) from community where community_name = ?", item.Name); err != nil {
		return err
	}
	if count > 0 {
		_, err := db.Exec("update community set introduction = ? where community_name = ?", item.Introduction, item.Name)
		return err
	}

	var nextCommunityID int64
	if err := db.Get(&nextCommunityID, "select coalesce(max(community_id), 0) + 1 from community"); err != nil {
		return err
	}
	_, err := db.Exec(
		"insert into community(community_id, community_name, introduction) values (?, ?, ?)",
		nextCommunityID,
		item.Name,
		item.Introduction,
	)
	return err
}
