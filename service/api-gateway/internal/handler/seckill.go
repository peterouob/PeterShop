package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/peterouob/seckill_service/api/seckillproto"
	"github.com/peterouob/seckill_service/pkg/middleware"
)

// activity_id and order_no travel as JSON strings: snowflake ids exceed the 2^53
// integers a JavaScript number can hold exactly.
type buyRequest struct {
	ActivityID int64 `json:"activity_id,string" binding:"required"`
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

	resp, err := h.client.Buy(c.Request.Context(), &seckillproto.BuyRequest{
		UserId:     userID,
		ActivityId: req.ActivityID,
	})
	if err != nil {
		fromGRPC(c, err)
		return
	}
	if resp == nil {
		fromGRPC(c, errors.New("empty response from seckill service"))
		return
	}

	ok(c, gin.H{
		"state":    resp.GetBuyState().String(),
		"msg":      resp.GetMsg(),
		"order_no": strconv.FormatInt(resp.GetOrderNo(), 10),
	})
}
