package model

import "gorm.io/gorm"

type UserLoginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type UserRegisterReq struct {
	Username      string `json:"username"`
	UserId        string `json:"user_id"`
	Password      string `json:"password"`
	CheckPassword string `json:"check_password"`
}

type User struct {
	gorm.Model
	Username string `json:"username"`
	UserId   string `json:"user_id"`
	Password string `json:"password"`
}

func (u User) TableName() string {
	return "user"
}
