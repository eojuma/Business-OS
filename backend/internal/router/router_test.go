package router

import (
	"testing"

	"github.com/businessos/backend/internal/config"
	"github.com/businessos/backend/internal/shared/permissions"
)

func TestEveryProtectedRouteHasAPermission(t *testing.T) {
	cfg := &config.Config{
		AppEnv:         "test",
		FrontendURL:    "http://localhost:3000",
		AllowedOrigins: []string{"http://localhost:3000"},
		JWTSecret:      "test-secret",
		JWTExpiryHours: 1,
	}
	engine, _ := New(nil, cfg)

	public := map[string]bool{
		"GET /health":                    true,
		"POST /api/v1/auth/register":     true,
		"POST /api/v1/auth/login":        true,
		"POST /api/v1/auth/google":       true,
		"POST /api/v1/auth/email/verify": true,
		"POST /api/v1/auth/email/resend": true,
		"POST /api/v1/business":          true,
	}

	for _, route := range engine.Routes() {
		key := route.Method + " " + route.Path
		if public[key] {
			continue
		}
		if _, ok := permissions.ForRoute(route.Method, route.Path); !ok {
			t.Errorf("protected route %s has no permission mapping", key)
		}
	}
}
