package response

import "github.com/gin-gonic/gin"

// Success writes a successful JSON response with the given status and data.
func Success(c *gin.Context, status int, data interface{}) {
	c.JSON(status, gin.H{
		"success": true,
		"data":    data,
	})
}

// Error writes an error JSON response with the given status and message.
func Error(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{
		"success": false,
		"error":   message,
	})
}

// ErrorWithCode writes an error JSON response with a stable, machine-readable
// code alongside the human-readable message, so clients can branch on the
// code instead of matching message text.
func ErrorWithCode(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{
		"success": false,
		"error":   message,
		"code":    code,
	})
}

// Paginated writes a paginated JSON response with the given data and pagination metadata.
func Paginated(c *gin.Context, status int, data interface{}, page, perPage int, total int64) {
	c.JSON(status, gin.H{
		"success": true,
		"data":    data,
		"meta": gin.H{
			"page":     page,
			"per_page": perPage,
			"total":    total,
		},
	})
}
