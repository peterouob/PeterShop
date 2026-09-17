package router

import (
	"github.com/gin-gonic/gin"
	"github.com/peterouob/seckill_service/pkg/auth"
	"github.com/peterouob/seckill_service/pkg/middleware"
	"github.com/peterouob/seckill_service/service/api-gateway/internal/handler"
	"go.uber.org/fx"
)

type Params struct {
	fx.In
	Engine  *gin.Engine
	Auth    *auth.Manager
	User    *handler.User
	Seckill *handler.Seckill
}

func register(p Params) {
	p.Engine.Use(middleware.CORS())

	v1 := p.Engine.Group("/api/v1")
	{
		v1.POST("/users/login", p.User.Login)
		v1.POST("/users/register", p.User.Register)
	}

	secured := v1.Group("", middleware.Auth(p.Auth))
	{
		secured.POST("/seckill/buy", p.Seckill.Buy)
	}
}

var Module = fx.Module("router",
	fx.Provide(
		handler.NewUser,
		handler.NewSeckill,
	),
	fx.Invoke(register),
)
