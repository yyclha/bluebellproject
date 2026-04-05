package mysql

import "bluebell/models"

func InitCommentTable() error {
	sqlStr := `CREATE TABLE IF NOT EXISTS comment (
		id bigint(20) NOT NULL AUTO_INCREMENT,
		comment_id bigint(20) NOT NULL,
		post_id bigint(20) NOT NULL,
		author_id bigint(20) NOT NULL,
		content varchar(2048) NOT NULL,
		create_time timestamp NULL DEFAULT CURRENT_TIMESTAMP,
		update_time timestamp NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		PRIMARY KEY (id),
		UNIQUE KEY idx_comment_id (comment_id),
		KEY idx_post_id (post_id),
		KEY idx_author_id (author_id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;`
	_, err := db.Exec(sqlStr)
	return err
}

func CreateComment(comment *models.Comment) error {
	sqlStr := `insert into comment(comment_id, post_id, author_id, content) values (?, ?, ?, ?)`
	_, err := db.Exec(sqlStr, comment.ID, comment.PostID, comment.AuthorID, comment.Content)
	return err
}

func GetCommentListByPostID(postID int64) (comments []*models.Comment, err error) {
	sqlStr := `select comment_id, post_id, author_id, content, create_time
	from comment
	where post_id = ?
	order by create_time asc`
	err = db.Select(&comments, sqlStr, postID)
	return
}

func DeleteCommentsByPostID(postID int64) error {
	sqlStr := `delete from comment where post_id = ?`
	_, err := db.Exec(sqlStr, postID)
	return err
}
