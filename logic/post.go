package logic

import (
	"bluebell/dao/milvus"
	"bluebell/dao/mysql"
	"bluebell/dao/redis"
	"bluebell/models"
	"bluebell/pkg/snowflake"
	"context"
	"errors"
	"strconv"
	"time"

	"go.uber.org/zap"
)

var ErrPostNotFound = errors.New("post not found")
var ErrDeletePostNoPermission = errors.New("delete post no permission")

func CreatePost(p *models.Post) (err error) {
	p.ID = snowflake.GenID()
	if err = mysql.CreatePost(p); err != nil {
		return err
	}
	return redis.CreatePost(p.ID, p.CommunityID)
}

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

func GetPostList(page, size int64) (data []*models.ApiPostDetail, err error) {
	posts, err := mysql.GetPostList(page, size)
	if err != nil {
		return nil, err
	}

	data = make([]*models.ApiPostDetail, 0, len(posts))
	for _, post := range posts {
		user, userErr := mysql.GetUserById(post.AuthorID)
		if userErr != nil {
			zap.L().Error("mysql.GetUserById(post.AuthorID) failed", zap.Int64("author_id", post.AuthorID), zap.Error(userErr))
			continue
		}

		community, communityErr := mysql.GetCommunityDetailByID(post.CommunityID)
		if communityErr != nil {
			zap.L().Error("mysql.GetCommunityDetailByID(post.CommunityID) failed", zap.Int64("community_id", post.CommunityID), zap.Error(communityErr))
			continue
		}

		data = append(data, &models.ApiPostDetail{
			AuthorName:      user.Username,
			Post:            post,
			CommunityDetail: community,
		})
	}
	return
}

func GetPostList2(p *models.ParamPostList) (data []*models.ApiPostDetail, err error) {
	ids, err := redis.GetPostIDsInOrder(p)
	if err != nil {
		return
	}
	if len(ids) == 0 {
		zap.L().Warn("redis.GetPostIDsInOrder(p) return 0 data")
		return
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
		user, userErr := mysql.GetUserById(post.AuthorID)
		if userErr != nil {
			zap.L().Error("mysql.GetUserById(post.AuthorID) failed", zap.Int64("author_id", post.AuthorID), zap.Error(userErr))
			continue
		}

		community, communityErr := mysql.GetCommunityDetailByID(post.CommunityID)
		if communityErr != nil {
			zap.L().Error("mysql.GetCommunityDetailByID(post.CommunityID) failed", zap.Int64("community_id", post.CommunityID), zap.Error(communityErr))
			continue
		}

		data = append(data, &models.ApiPostDetail{
			AuthorName:      user.Username,
			VoteNum:         voteData[idx],
			Post:            post,
			CommunityDetail: community,
		})
	}
	return
}

func GetCommunityPostList(p *models.ParamPostList) (data []*models.ApiPostDetail, err error) {
	ids, err := redis.GetCommunityPostIDsInOrder(p)
	if err != nil {
		return
	}
	if len(ids) == 0 {
		zap.L().Warn("redis.GetCommunityPostIDsInOrder(p) return 0 data")
		return
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
		user, userErr := mysql.GetUserById(post.AuthorID)
		if userErr != nil {
			zap.L().Error("mysql.GetUserById(post.AuthorID) failed", zap.Int64("author_id", post.AuthorID), zap.Error(userErr))
			continue
		}

		community, communityErr := mysql.GetCommunityDetailByID(post.CommunityID)
		if communityErr != nil {
			zap.L().Error("mysql.GetCommunityDetailByID(post.CommunityID) failed", zap.Int64("community_id", post.CommunityID), zap.Error(communityErr))
			continue
		}

		data = append(data, &models.ApiPostDetail{
			AuthorName:      user.Username,
			VoteNum:         voteData[idx],
			Post:            post,
			CommunityDetail: community,
		})
	}
	return
}

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
