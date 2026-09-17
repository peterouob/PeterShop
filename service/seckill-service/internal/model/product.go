package model

type Product struct {
	ProductId   string `json:"product_id"`
	Name        string `json:"name"`
	Stock       int    `json:"stock"`
	Price       int    `json:"price"`
	Description string `json:"description"`
	Img         string `json:"img"`
}

func (p Product) TableName() string {
	return "product"
}

type Stock struct {
	ProductId string `json:"product_id"`
	Stock     int    `json:"stock"`
}

func (s Stock) TableName() string {
	return "stock"
}

type SeckillReq struct {
	UserID    string `json:"user_id" binding:"required"`
	ProductID string `json:"product_id" binding:"required"`
}
