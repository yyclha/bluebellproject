package models

type User struct {
	UserID      int64  `db:"user_id" json:"user_id,string"`
	Username    string `db:"username" json:"username"`
	Password    string `db:"password" json:"-"`
	Email       string `db:"email" json:"email"`
	Gender      int8   `db:"gender" json:"gender"`
	AvatarURL   string `db:"avatar_url" json:"avatar_url"`
	Bio         string `db:"bio" json:"bio"`
	CreateTime  string `db:"create_time" json:"create_time,omitempty"`
	UpdateTime  string `db:"update_time" json:"update_time,omitempty"`
	Token       string `json:"token,omitempty"`
}

type ParamUpdateProfile struct {
	Username  string `json:"username" binding:"required,min=2,max=24"`
	Email     string `json:"email" binding:"omitempty,email,max=64"`
	Gender    int8   `json:"gender" binding:"oneof=0 1 2"`
	AvatarURL string `json:"avatar_url" binding:"omitempty,max=255"`
	Bio       string `json:"bio" binding:"omitempty,max=200"`
}

type ParamChangePassword struct {
	OldPassword string `json:"old_password" binding:"required,min=6,max=64"`
	NewPassword string `json:"new_password" binding:"required,min=6,max=64"`
	RePassword  string `json:"re_password" binding:"required,eqfield=NewPassword"`
}
