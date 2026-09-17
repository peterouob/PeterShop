package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/peterouob/seckill_service/api/userproto"
)

type loginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type registerRequest struct {
	Username      string `json:"username" binding:"required"`
	Password      string `json:"password" binding:"required,min=8"`
	CheckPassword string `json:"check_password" binding:"required,eqfield=Password"`
}

type User struct {
	client userproto.UserServiceClient
}

func NewUser(client userproto.UserServiceClient) *User {
	return &User{client: client}
}

func (h *User) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err)
		return
	}

	resp, err := h.client.UserLogin(c.Request.Context(), &userproto.UserLoginReq{
		Username: req.Username,
		Password: req.Password,
	})
	if err != nil {
		fromGRPC(c, err)
		return
	}

	ok(c, gin.H{"msg": resp.GetMsg(), "token": resp.GetToken()})
}

func (h *User) Register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err)
		return
	}

	resp, err := h.client.UserRegister(c.Request.Context(), &userproto.UserRegisterReq{
		Username:      req.Username,
		Password:      req.Password,
		CheckPassword: req.CheckPassword,
	})
	if err != nil {
		fromGRPC(c, err)
		return
	}

	ok(c, gin.H{"msg": resp.GetMsg()})
}
