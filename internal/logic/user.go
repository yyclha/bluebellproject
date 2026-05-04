package logic

import (
	"gamebase/internal/dao/mysql"
	"gamebase/internal/models"
	"gamebase/pkg/jwt"
	"gamebase/pkg/snowflake"
)

func SignUp(p *models.ParamSignUp) error {
	if err := mysql.CheckUserExist(p.Username); err != nil {
		return err
	}
	userID := snowflake.GenID()
	user := &models.User{
		UserID:   userID,
		Username: p.Username,
		Password: p.Password,
	}
	return mysql.InsertUser(user)
}

func Login(p *models.ParamLogin) (*models.User, error) {
	user := &models.User{
		Username: p.Username,
		Password: p.Password,
	}
	if err := mysql.Login(user); err != nil {
		return nil, err
	}
	token, err := jwt.GenToken(user.UserID, user.Username)
	if err != nil {
		return nil, err
	}
	user.Token = token
	return user, nil
}
