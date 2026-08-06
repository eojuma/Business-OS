package middleware

import (
	"net/http"

	"github.com/businessos/backend/internal/config"
	"github.com/gin-gonic/gin"
)


func CORS(cfg *config.Config) gin.HandlerFunc {
	allowedOrigin := "http://localhost:3000" 

	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", allowedOrigin)
		c.Header("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}