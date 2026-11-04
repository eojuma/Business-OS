package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/businessos/backend/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func testToken(t *testing.T, cfg *config.Config, role string, userID string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"user_id":     userID,
		"business_id": uuid.NewString(),
		"role":        role,
		"exp":         time.Now().Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(cfg.JWTSecret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

func TestRequireRoutePermissionDeniesAndAllows(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		role string
		want int
	}{
		{"cashier", http.StatusForbidden},
		{"accountant", http.StatusOK},
		{"owner", http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			router := gin.New()
			group := router.Group("/api/v1")
			group.Use(func(c *gin.Context) {
				c.Set(ContextRoleKey, tt.role)
				c.Next()
			}, RequireRoutePermission())
			group.GET("/finance/summary", func(c *gin.Context) {
				c.Status(http.StatusOK)
			})

			recorder := httptest.NewRecorder()
			request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/finance/summary", nil)
			router.ServeHTTP(recorder, request)

			if recorder.Code != tt.want {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.want)
			}
		})
	}
}

func TestRequireRoutePermissionFailsClosedForUnmappedRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	group := router.Group("/api/v1")
	group.Use(func(c *gin.Context) {
		c.Set(ContextRoleKey, "owner")
		c.Next()
	}, RequireRoutePermission())
	group.GET("/unmapped", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/unmapped", nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
}

func TestRequireAuthUsesLiveRoleOnEachRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{JWTSecret: "test-secret", JWTExpiryHours: 1}
	userID := uuid.NewString()
	liveRole := "cashier"

	router := gin.New()
	group := router.Group("/api/v1")
	group.Use(RequireAuth(cfg, func(string) (string, bool, error) {
		return liveRole, true, nil
	}), RequireRoutePermission())
	group.GET("/finance/summary", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	request := func() int {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/finance/summary", nil)
		// The token carries a stale owner claim; the loader must win.
		req.Header.Set("Authorization", "Bearer "+testToken(t, cfg, "owner", userID))
		router.ServeHTTP(recorder, req)
		return recorder.Code
	}

	if got := request(); got != http.StatusForbidden {
		t.Fatalf("cashier request status = %d, want %d", got, http.StatusForbidden)
	}
	liveRole = "accountant"
	if got := request(); got != http.StatusOK {
		t.Fatalf("accountant request status = %d, want %d", got, http.StatusOK)
	}
}

func TestRequireAuthRejectsInactiveUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{JWTSecret: "test-secret", JWTExpiryHours: 1}

	router := gin.New()
	router.GET("/private", RequireAuth(cfg, func(string) (string, bool, error) {
		return "owner", false, nil
	}), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	recorder := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/private", nil)
	req.Header.Set("Authorization", "Bearer "+testToken(t, cfg, "owner", uuid.NewString()))
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
}
