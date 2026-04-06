package logic

import (
	"bluebell/dao/mysql"
	"bluebell/dao/redis"
	"bluebell/models"
	"bluebell/pkg/snowflake"
)

func CreateComment(p *models.ParamCreateComment, authorID int64) error {
	comment := &models.Comment{
		ID:       snowflake.GenID(),
		PostID:   p.PostID,
		AuthorID: authorID,
		Content:  p.Content,
	}
	if err := mysql.CreateComment(comment); err != nil {
		return err
	}
	return redis.AddCommentScore(comment.PostID)
}

func GetCommentListByPostID(postID int64) ([]*models.ApiCommentDetail, error) {
	comments, err := mysql.GetCommentListByPostID(postID)
	if err != nil {
		return nil, err
	}

	data := make([]*models.ApiCommentDetail, 0, len(comments))
	for _, comment := range comments {
		user, userErr := mysql.GetUserById(comment.AuthorID)
		if userErr != nil {
			continue
		}
		data = append(data, &models.ApiCommentDetail{
			Comment:    comment,
			AuthorName: user.Username,
		})
	}
	return data, nil
}
