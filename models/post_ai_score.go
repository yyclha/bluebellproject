package models

import "time"

type PostAIScore struct {
	ID           int64     `db:"id"`
	PostID       int64     `db:"post_id"`
	Model        string    `db:"model"`
	RawScore     float64   `db:"raw_score"`
	ScoreWeight  float64   `db:"score_weight"`
	ScoreDelta   float64   `db:"score_delta"`
	ResponseText string    `db:"response_text"`
	CreateTime   time.Time `db:"create_time"`
	UpdateTime   time.Time `db:"update_time"`
}
