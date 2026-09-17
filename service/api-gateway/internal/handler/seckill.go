package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/peterouob/seckill_service/api/seckillproto"
	"github.com/peterouob/seckill_service/pkg/middleware"
)

type buyRequest struct {
	ProductID string `json:"product_id" binding:"required"`
}

type Seckill struct {
	client seckillproto.SeckillServiceClient
}

func NewSeckill(client seckillproto.SeckillServiceClient) *Seckill {
	return &Seckill{client: client}
}

func (h *Seckill) Buy(c *gin.Context) {
	userID, authenticated := middleware.UserID(c)
	if !authenticated {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}

	var req buyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err)
		return
	}

	resp, err := h.client.Seckill(c.Request.Context(), &seckillproto.SeckillRequest{
		UserId:    userID,
		ProductId: req.ProductID,
	})
	if err != nil {
		fromGRPC(c, err)
		return
	}
	if resp == nil {
		fromGRPC(c, errors.New("empty response from seckill service"))
		return
	}

	ok(c, gin.H{"state": resp.GetBuyState().String(), "msg": resp.GetMsg()})
}
