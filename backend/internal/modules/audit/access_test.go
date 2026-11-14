package audit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/businessos/backend/internal/config"
	"github.com/businessos/backend/internal/shared/middleware"
	"github.com/businessos/backend/internal/shared/permissions"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// accessToken documents its scenario and asserts the expected audit behaviour.
func accessToken(t *testing.T, secret, role string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"user_id":     uuid.NewString(),
		"business_id": uuid.NewString(),
		"role":        role,
		"exp":         time.Now().Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

// TestMountedAuditEndpointAccessControl documents its scenario and asserts the expected audit behaviour.
func TestMountedAuditEndpointAccessControl(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secret := uuid.NewString()
	cfg := &config.Config{JWTSecret: secret, JWTExpiryHours: 1}

	router := gin.New()
	group := router.Group("/api/v1")
	group.Use(middleware.RequireAuth(cfg), middleware.RequireRoutePermission())
	group.GET("/audit", NewHandler(&fakeService{}).List)

	tests := []struct {
		name   string
		role   string
		token  bool
		status int
	}{
		{"owner allowed", permissions.RoleOwner, true, http.StatusOK},
		{"admin allowed", permissions.RoleAdmin, true, http.StatusOK},
		{"manager denied", permissions.RoleManager, true, http.StatusForbidden},
		{"cashier denied", permissions.RoleCashier, true, http.StatusForbidden},
		{"accountant denied", permissions.RoleAccountant, true, http.StatusForbidden},
		{"warehouse denied", permissions.RoleWarehouse, true, http.StatusForbidden},
		{"unauthenticated denied", "", false, http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/audit", nil)
			if tt.token {
				request.Header.Set("Authorization", "Bearer "+accessToken(t, secret, tt.role))
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != tt.status {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.status)
			}
		})
	}
}
