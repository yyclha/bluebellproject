package logic

import (
	"bluebell/internal/dao/milvus"
	"bluebell/internal/dao/mysql"
	"bluebell/internal/dao/redis"
	"bluebell/internal/models"
	"bluebell/pkg/snowflake"
	"context"
	"errors"
	"strconv"
	"time"

	"go.uber.org/zap"
)

var ErrPostNotFound = errors.New("post not found")
var ErrDeletePostNoPermission = errors.New("delete post no permission")

// CreatePost 创建帖子并写入存储。
func CreatePost(p *models.Post) (err error) {
	p.ID = snowflake.GenID()
	if err = mysql.CreatePost(p); err != nil {
		return err
	}
	return redis.CreatePost(p.ID, p.CommunityID)
}

// GetPostById 根据帖子 ID 获取帖子详情。
func GetPostById(pid int64) (data *models.ApiPostDetail, err error) {
	post, err := mysql.GetPostById(pid)
	if err != nil {
		zap.L().Error("mysql.GetPostById(pid) failed", zap.Int64("pid", pid), zap.Error(err))
		return
	}

	user, err := mysql.GetUserById(post.AuthorID)
	if err != nil {
		zap.L().Error("mysql.GetUserById(post.AuthorID) failed", zap.Int64("author_id", post.AuthorID), zap.Error(err))
		return
	}

	community, err := mysql.GetCommunityDetailByID(post.CommunityID)
	if err != nil {
		zap.L().Error("mysql.GetCommunityDetailByID(post.CommunityID) failed", zap.Int64("community_id", post.CommunityID), zap.Error(err))
		return
	}

	voteNum := int64(0)
	voteData, voteErr := redis.GetPostVoteData([]string{strconv.FormatInt(pid, 10)})
	if voteErr != nil {
		zap.L().Warn("redis.GetPostVoteData failed", zap.Int64("pid", pid), zap.Error(voteErr))
	} else if len(voteData) > 0 {
		voteNum = voteData[0]
	}

	data = &models.ApiPostDetail{
		AuthorName:      user.Username,
		VoteNum:         voteNum,
		Post:            post,
		CommunityDetail: community,
	}
	return
}

// GetPostList 按分页参数获取帖子列表。
func GetPostList(page, size int64) (data []*models.ApiPostDetail, err error) {
	posts, err := mysql.GetPostList(page, size)
	if err != nil {
		return nil, err
	}

	data = make([]*models.ApiPostDetail, 0, len(posts))
	for _, post := range posts {
		authorName := ""
		user, userErr := mysql.GetUserById(post.AuthorID)
		if userErr != nil {
			zap.L().Warn("mysql.GetUserById(post.AuthorID) failed", zap.Int64("author_id", post.AuthorID), zap.Error(userErr))
		} else {
			authorName = user.Username
		}

		community := &models.CommunityDetail{}
		community, communityErr := mysql.GetCommunityDetailByID(post.CommunityID)
		if communityErr != nil {
			zap.L().Warn("mysql.GetCommunityDetailByID(post.CommunityID) failed", zap.Int64("community_id", post.CommunityID), zap.Error(communityErr))
			community = &models.CommunityDetail{ID: post.CommunityID}
		}

		data = append(data, &models.ApiPostDetail{
			AuthorName:      authorName,
			Post:            post,
			CommunityDetail: community,
		})
	}
	return
}

// GetPostList2 按扩展排序参数获取帖子列表。
func GetPostList2(p *models.ParamPostList) (data []*models.ApiPostDetail, err error) {
	ids, err := redis.GetPostIDsInOrder(p)
	if err != nil {
		return
	}
	if len(ids) == 0 {
		zap.L().Warn("redis.GetPostIDsInOrder(p) return 0 data")
		return GetPostList(p.Page, p.Size)
	}

	posts, err := mysql.GetPostListByIDs(ids)
	if err != nil {
		return
	}

	voteData, err := redis.GetPostVoteData(ids)
	if err != nil {
		return
	}

	data = make([]*models.ApiPostDetail, 0, len(posts))
	for idx, post := range posts {
		authorName := ""
		user, userErr := mysql.GetUserById(post.AuthorID)
		if userErr != nil {
			zap.L().Warn("mysql.GetUserById(post.AuthorID) failed", zap.Int64("author_id", post.AuthorID), zap.Error(userErr))
		} else {
			authorName = user.Username
		}

		community := &models.CommunityDetail{}
		community, communityErr := mysql.GetCommunityDetailByID(post.CommunityID)
		if communityErr != nil {
			zap.L().Warn("mysql.GetCommunityDetailByID(post.CommunityID) failed", zap.Int64("community_id", post.CommunityID), zap.Error(communityErr))
			community = &models.CommunityDetail{ID: post.CommunityID}
		}

		data = append(data, &models.ApiPostDetail{
			AuthorName:      authorName,
			VoteNum:         voteData[idx],
			Post:            post,
			CommunityDetail: community,
		})
	}
	return
}

// GetCommunityPostList 获取指定社区下的帖子列表。
func GetCommunityPostList(p *models.ParamPostList) (data []*models.ApiPostDetail, err error) {
	ids, err := redis.GetCommunityPostIDsInOrder(p)
	if err != nil {
		return
	}
	if len(ids) == 0 {
		zap.L().Warn("redis.GetCommunityPostIDsInOrder(p) return 0 data")
		posts, postErr := mysql.GetPostListByCommunityID(p.CommunityID, p.Page, p.Size)
		if postErr != nil {
			return nil, postErr
		}
		return buildPostDetails(posts, nil)
	}

	posts, err := mysql.GetPostListByIDs(ids)
	if err != nil {
		return
	}

	voteData, err := redis.GetPostVoteData(ids)
	if err != nil {
		return
	}

	data = make([]*models.ApiPostDetail, 0, len(posts))
	for idx, post := range posts {
		authorName := ""
		user, userErr := mysql.GetUserById(post.AuthorID)
		if userErr != nil {
			zap.L().Warn("mysql.GetUserById(post.AuthorID) failed", zap.Int64("author_id", post.AuthorID), zap.Error(userErr))
		} else {
			authorName = user.Username
		}

		community := &models.CommunityDetail{}
		community, communityErr := mysql.GetCommunityDetailByID(post.CommunityID)
		if communityErr != nil {
			zap.L().Warn("mysql.GetCommunityDetailByID(post.CommunityID) failed", zap.Int64("community_id", post.CommunityID), zap.Error(communityErr))
			community = &models.CommunityDetail{ID: post.CommunityID}
		}

		data = append(data, &models.ApiPostDetail{
			AuthorName:      authorName,
			VoteNum:         voteData[idx],
			Post:            post,
			CommunityDetail: community,
		})
	}
	return
}

// buildPostDetails 聚合帖子、作者、社区和投票数信息。
func buildPostDetails(posts []*models.Post, voteData []int64) (data []*models.ApiPostDetail, err error) {
	data = make([]*models.ApiPostDetail, 0, len(posts))
	for idx, post := range posts {
		authorName := ""
		user, userErr := mysql.GetUserById(post.AuthorID)
		if userErr != nil {
			zap.L().Warn("mysql.GetUserById(post.AuthorID) failed", zap.Int64("author_id", post.AuthorID), zap.Error(userErr))
		} else {
			authorName = user.Username
		}

		community, communityErr := mysql.GetCommunityDetailByID(post.CommunityID)
		if communityErr != nil {
			zap.L().Warn("mysql.GetCommunityDetailByID(post.CommunityID) failed", zap.Int64("community_id", post.CommunityID), zap.Error(communityErr))
			community = &models.CommunityDetail{ID: post.CommunityID}
		}

		voteNum := int64(0)
		if idx < len(voteData) {
			voteNum = voteData[idx]
		}

		data = append(data, &models.ApiPostDetail{
			AuthorName:      authorName,
			VoteNum:         voteNum,
			Post:            post,
			CommunityDetail: community,
		})
	}
	return data, nil
}

// GetPostListNew 按新的排序规则聚合帖子列表数据。
func GetPostListNew(p *models.ParamPostList) (data []*models.ApiPostDetail, err error) {
	if p.CommunityID == 0 {
		data, err = GetPostList2(p)
	} else {
		data, err = GetCommunityPostList(p)
	}
	if err != nil {
		zap.L().Error("GetPostListNew failed", zap.Error(err))
		return nil, err
	}
	return
}

// DeletePostByID 删除指定帖子及其关联数据。
func DeletePostByID(pid, userID int64) error {
	post, err := mysql.GetPostById(pid)
	if err != nil {
		return ErrPostNotFound
	}
	if post.AuthorID != userID {
		return ErrDeletePostNoPermission
	}

	if err := mysql.DeleteCommentsByPostID(pid); err != nil {
		return err
	}
	if err := mysql.DeletePostAIScoreByPostID(pid); err != nil {
		return err
	}
	if err := mysql.DeletePostByID(pid); err != nil {
		return err
	}
	if err := redis.DeletePost(pid, post.CommunityID); err != nil {
		zap.L().Warn("redis.DeletePost failed", zap.Int64("post_id", pid), zap.Error(err))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := milvus.DeletePostChunks(ctx, pid); err != nil {
		zap.L().Warn("milvus.DeletePostChunks failed", zap.Int64("post_id", pid), zap.Error(err))
	}
	return nil
}
