package model

import (
	"time"
)

type Order struct {
	OrderId   string    `json:"uniqueId" gorm:"uniqueIndex:idx_user_product"`
	UserId    string    `json:"user_id"`
	ProductId string    `json:"product_id"`
	CreateAt  time.Time `json:"create_at"`
}

func (o Order) TableName() string {
	return "orders"
}
