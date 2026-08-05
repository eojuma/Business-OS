package middleware

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)


func CurrentBusinessID(c *gin.Context) (uuid.UUID, error) {
	raw, exists := c.Get(ContextBusinessIDKey)
	if !exists {
		return uuid.Nil, errors.New("missing business context")
	}
	str, ok := raw.(string)
	if !ok {
		return uuid.Nil, errors.New("invalid business context")
	}
	return uuid.Parse(str)
}