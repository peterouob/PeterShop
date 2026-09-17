package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/peterouob/seckill_service/pkg/auth"
)

const bearerPrefix = "Bearer "

func Auth(manager *auth.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, ok := bearerToken(c.GetHeader("Authorization"))
		if !ok {
			abortUnauthorized(c, "missing or malformed Authorization header")
			return
		}

		claims, err := manager.Verify(token)
		if err != nil {
			abortUnauthorized(c, "invalid or expired token")
			return
		}

		c.Set(ctxKeyUserID, claims.UserID)
		c.Set(ctxKeyAccessID, claims.AccessID)
		c.Next()
	}
}

func bearerToken(header string) (string, bool) {
	if len(header) <= len(bearerPrefix) || !strings.EqualFold(header[:len(bearerPrefix)], bearerPrefix) {
		return "", false
	}
	token := strings.TrimSpace(header[len(bearerPrefix):])
	return token, token != ""
}

func abortUnauthorized(c *gin.Context, msg string) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": msg})
}
