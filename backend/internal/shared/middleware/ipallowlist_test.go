package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequireIPAllowlistAllowsListedAndRejectsOthers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequireIPAllowlist([]string{"10.0.0.0/8", "203.0.113.7"}))
	router.POST("/callback", func(c *gin.Context) { c.Status(http.StatusOK) })

	cases := []struct {
		name       string
		remoteAddr string
		want       int
	}{
		{"cidr member", "10.1.2.3:5555", http.StatusOK},
		{"exact ip", "203.0.113.7:5555", http.StatusOK},
		{"outside", "192.168.1.1:5555", http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/callback", nil)
			req.RemoteAddr = tc.remoteAddr
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d", w.Code, tc.want)
			}
		})
	}
}

func TestRequireIPAllowlistEmptyAllowsAll(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequireIPAllowlist(nil))
	router.POST("/callback", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/callback", nil)
	req.RemoteAddr = "198.51.100.1:5555"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}
