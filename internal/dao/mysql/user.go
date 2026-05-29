package mysql

import (
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"gamebase/internal/models"
	"strings"
)

const secret = "liwenzhou.com"

func CheckUserExist(username string) error {
	sqlStr := `select count(user_id) from user where username = ?`
	var count int64
	if err := db.Get(&count, sqlStr, username); err != nil {
		return err
	}
	if count > 0 {
		return ErrorUserExist
	}
	return nil
}

func CheckUserExistExceptUserID(username string, userID int64) error {
	sqlStr := `select count(user_id) from user where username = ? and user_id <> ?`
	var count int64
	if err := db.Get(&count, sqlStr, username, userID); err != nil {
		return err
	}
	if count > 0 {
		return ErrorUserExist
	}
	return nil
}

func InsertUser(user *models.User) error {
	user.Password = encryptPassword(user.Password)
	sqlStr := `insert into user(user_id, username, password, email, gender, avatar_url, bio) values(?,?,?,?,?,?,?)`
	_, err := db.Exec(sqlStr, user.UserID, user.Username, user.Password, user.Email, user.Gender, user.AvatarURL, user.Bio)
	return err
}

func encryptPassword(oPassword string) string {
	h := md5.New()
	h.Write([]byte(secret))
	return hex.EncodeToString(h.Sum([]byte(oPassword)))
}

func Login(user *models.User) error {
	oPassword := user.Password
	sqlStr := `select user_id, username, password, email, gender, ifnull(avatar_url, '') as avatar_url, ifnull(bio, '') as bio from user where username=?`
	err := db.Get(user, sqlStr, user.Username)
	if err == sql.ErrNoRows {
		return ErrorUserNotExist
	}
	if err != nil {
		return err
	}

	password := encryptPassword(oPassword)
	if password != user.Password {
		return ErrorInvalidPassword
	}
	return nil
}

func GetUserById(uid int64) (*models.User, error) {
	user := new(models.User)
	sqlStr := `select user_id, username, ifnull(email, '') as email, gender, ifnull(avatar_url, '') as avatar_url, ifnull(bio, '') as bio, create_time, update_time from user where user_id = ?`
	err := db.Get(user, sqlStr, uid)
	return user, err
}

func UpdateUserProfile(userID int64, p *models.ParamUpdateProfile) error {
	sqlStr := `update user set username=?, email=?, gender=?, avatar_url=?, bio=? where user_id=?`
	_, err := db.Exec(sqlStr, strings.TrimSpace(p.Username), strings.TrimSpace(p.Email), p.Gender, strings.TrimSpace(p.AvatarURL), strings.TrimSpace(p.Bio), userID)
	return err
}

func CheckUserPassword(userID int64, oldPassword string) error {
	var encrypted string
	sqlStr := `select password from user where user_id = ?`
	if err := db.Get(&encrypted, sqlStr, userID); err != nil {
		if err == sql.ErrNoRows {
			return ErrorUserNotExist
		}
		return err
	}
	if encryptPassword(oldPassword) != encrypted {
		return ErrorInvalidPassword
	}
	return nil
}

func UpdateUserPassword(userID int64, newPassword string) error {
	sqlStr := `update user set password=? where user_id=?`
	_, err := db.Exec(sqlStr, encryptPassword(newPassword), userID)
	return err
}

func InitUserProfileColumns() error {
	if err := addColumnIfNotExists("user", "avatar_url", "varchar(255) null after gender"); err != nil {
		return err
	}
	if err := addColumnIfNotExists("user", "bio", "varchar(200) null after avatar_url"); err != nil {
		return err
	}
	return nil
}

func addColumnIfNotExists(tableName, columnName, columnDef string) error {
	var count int
	sqlStr := `select count(1) from information_schema.columns where table_schema = database() and table_name = ? and column_name = ?`
	if err := db.Get(&count, sqlStr, tableName, columnName); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	_, err := db.Exec(`alter table ` + tableName + ` add column ` + columnName + ` ` + columnDef)
	return err
}
