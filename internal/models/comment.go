package models

import "time"

type Comment struct {
	ID         int64     `json:"id,string" db:"comment_id"`
	PostID     int64     `json:"post_id,string" db:"post_id"`
	AuthorID   int64     `json:"author_id" db:"author_id"`
	Content    string    `json:"content" db:"content" binding:"required"`
	CreateTime time.Time `json:"create_time" db:"create_time"`
}

type ApiCommentDetail struct {
	*Comment
	AuthorName string `json:"author_name"`
}

type ParamCreateComment struct {
	PostID  int64  `json:"post_id,string" binding:"required"`
	Content string `json:"content" binding:"required"`
}
