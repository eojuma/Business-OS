package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/businessos/backend/internal/config"
	"github.com/businessos/backend/internal/shared/permissions"
	"github.com/businessos/backend/internal/shared/response"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

const (
	ContextUserIDKey     = "user_id"
	ContextBusinessIDKey = "business_id"
	ContextRoleKey       = "role"
)

// RoleLoader resolves the authoritative role for a user. Supplying one to
// RequireAuth makes role changes effective on the very next request instead of
// waiting for the current token to expire.
type RoleLoader func(userID string) (role string, active bool, err error)

// RoleFromDB loads the live role and active flag from the users table.
func RoleFromDB(db *gorm.DB) RoleLoader {
	return func(userID string) (string, bool, error) {
		var user struct {
			Role   string
			Active bool
		}
		err := db.Table("users").Select("role, active").Where("id = ?", userID).Take(&user).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", false, nil
		}
		if err != nil {
			return "", false, err
		}
		return user.Role, user.Active, nil
	}
}

// RequireAuth validates the bearer token. When a RoleLoader is supplied it
// also refreshes the caller role from the database for this request.
func RequireAuth(cfg *config.Config, roleLoaders ...RoleLoader) gin.HandlerFunc {
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
		}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
		if err != nil || !token.Valid {
			response.Error(c, 401, "invalid or expired token")
			c.Abort()
			return
		}

		c.Set(ContextUserIDKey, claims["user_id"])
		c.Set(ContextBusinessIDKey, claims["business_id"])
		c.Set(ContextRoleKey, claims["role"])

		if len(roleLoaders) > 0 {
			userID, _ := claims["user_id"].(string)
			if userID == "" {
				response.Error(c, 401, "invalid or expired token")
				c.Abort()
				return
			}
			role, active, err := roleLoaders[0](userID)
			if err != nil {
				response.Error(c, http.StatusInternalServerError, "failed to verify permissions")
				c.Abort()
				return
			}
			if !active {
				response.Error(c, http.StatusForbidden, "user account is inactive")
				c.Abort()
				return
			}
			c.Set(ContextRoleKey, role)
		}

		c.Next()
	}
}

// RequireRole restricts a route to specific roles. Prefer RequirePermission or
// RequireRoutePermission so the central matrix stays authoritative.
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

// RequirePermission enforces one explicit capability from the matrix.
func RequirePermission(permission permissions.Permission) gin.HandlerFunc {
	return func(c *gin.Context) {
		role, _ := c.Get(ContextRoleKey)
		roleStr, _ := role.(string)
		if !permissions.Allows(roleStr, permission) {
			response.Error(c, http.StatusForbidden, "insufficient permissions")
			c.Abort()
			return
		}
		c.Next()
	}
}

// RequireRoutePermission maps the matched route to its permission and enforces
// it. Unmapped protected routes fail closed.
func RequireRoutePermission() gin.HandlerFunc {
	return func(c *gin.Context) {
		permission, ok := permissions.ForRoute(c.Request.Method, c.FullPath())
		if !ok {
			response.Error(c, http.StatusInternalServerError, "route permission is not configured")
			c.Abort()
			return
		}
		role, _ := c.Get(ContextRoleKey)
		roleStr, _ := role.(string)
		if !permissions.Allows(roleStr, permission) {
			response.Error(c, http.StatusForbidden, "insufficient permissions")
			c.Abort()
			return
		}
		c.Next()
	}
}
