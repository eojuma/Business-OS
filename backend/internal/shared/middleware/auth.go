package middleware

import (
	"strings"

	"github.com/businessos/backend/internal/config"
	"github.com/businessos/backend/internal/shared/response"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const (
	ContextUserIDKey     = "user_id"
	ContextBusinessIDKey = "business_id"
	ContextRoleKey       = "role"
)

func RequireAuth(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" || !strings.HasPrefix(header, "Bearer ") {
			response.Error(c, 401, "missing or invalid authorization header")
			c.Abort()
			return
		}

		tokenString := strings.TrimPrefix(header, "Bearer ")

		claims := jwt.MapClaims{}
		token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
			return []byte(cfg.JWTSecret), nil
		})
		if err != nil || !token.Valid {
			response.Error(c, 401, "invalid or expired token")
			c.Abort()
			return
		}

		c.Set(ContextUserIDKey, claims["user_id"])
		c.Set(ContextBusinessIDKey, claims["business_id"])
		c.Set(ContextRoleKey, claims["role"])

		c.Next()
	}
}

// RequireRole restricts a route to specific roles. Use after RequireAuth.
func RequireRole(roles ...string) gin.HandlerFunc {
	allowed := make(map[string]bool)
	for _, r := range roles {
		allowed[r] = true
	}

	return func(c *gin.Context) {
		role, _ := c.Get(ContextRoleKey)
		roleStr, _ := role.(string)

		if !allowed[roleStr] {
			response.Error(c, 403, "insufficient permissions")
			c.Abort()
			return
		}
		c.Next()
	}
}