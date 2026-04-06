package mysql

import "bluebell/models"

func InitPostAIScoreTable() error {
	sqlStr := `CREATE TABLE IF NOT EXISTS post_ai_score (
		id bigint(20) NOT NULL AUTO_INCREMENT,
		post_id bigint(20) NOT NULL,
		model varchar(128) NOT NULL,
		raw_score decimal(10,2) NOT NULL DEFAULT 0,
		score_weight decimal(10,2) NOT NULL DEFAULT 0,
		score_delta decimal(10,2) NOT NULL DEFAULT 0,
		response_text varchar(2048) NOT NULL DEFAULT '',
		create_time timestamp NULL DEFAULT CURRENT_TIMESTAMP,
		update_time timestamp NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		PRIMARY KEY (id),
		UNIQUE KEY idx_post_id (post_id),
		KEY idx_model (model)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;`
	_, err := db.Exec(sqlStr)
	return err
}

func UpsertPostAIScore(score *models.PostAIScore) error {
	sqlStr := `insert into post_ai_score(post_id, model, raw_score, score_weight, score_delta, response_text)
	values (?, ?, ?, ?, ?, ?)
	on duplicate key update
		model = values(model),
		raw_score = values(raw_score),
		score_weight = values(score_weight),
		score_delta = values(score_delta),
		response_text = values(response_text)`
	_, err := db.Exec(sqlStr, score.PostID, score.Model, score.RawScore, score.ScoreWeight, score.ScoreDelta, score.ResponseText)
	return err
}

func DeletePostAIScoreByPostID(postID int64) error {
	sqlStr := `delete from post_ai_score where post_id = ?`
	_, err := db.Exec(sqlStr, postID)
	return err
}
