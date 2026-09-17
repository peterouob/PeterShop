package middleware

import "github.com/gin-gonic/gin"

const (
	ctxKeyUserID   = "auth.user_id"
	ctxKeyAccessID = "auth.access_id"
)

func UserID(c *gin.Context) (string, bool) {
	v, ok := c.Get(ctxKeyUserID)
	if !ok {
		return "", false
	}
	id, ok := v.(string)
	return id, ok && id != ""
}

func MustUserID(c *gin.Context) string {
	id, _ := UserID(c)
	return id
}

func AccessID(c *gin.Context) (string, bool) {
	v, ok := c.Get(ctxKeyAccessID)
	if !ok {
		return "", false
	}
	id, ok := v.(string)
	return id, ok
}
