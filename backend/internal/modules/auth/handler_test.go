package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type stubService struct{ googleErr error }

func (s *stubService) Register(RegisterInput) (*AuthResult, error) { return nil, nil }
func (s *stubService) Login(LoginInput) (*AuthResult, error)       { return nil, nil }
func (s *stubService) GoogleLogin(context.Context, GoogleLoginInput) (*AuthResult, error) {
	return nil, s.googleErr
}
func (s *stubService) CreateTeamUser(RegisterInput) (*User, error) { return nil, nil }
func (s *stubService) ListTeam(uuid.UUID) ([]User, error)          { return nil, nil }
func (s *stubService) GetByID(uuid.UUID) (*User, error)            { return nil, nil }

func TestGoogleHandlerReturnsStableErrorCodes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		err  error
		code string
	}{
		{name: "workspace required", err: ErrWorkspaceRequired, code: "workspace_required"},
		{name: "account conflict", err: ErrGoogleAccountConflict, code: "google_account_conflict"},
		{name: "link requires password", err: ErrGoogleLinkRequiresPassword, code: "google_link_requires_password"},
		{name: "email taken", err: ErrEmailTaken, code: "email_taken"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewHandler(&stubService{googleErr: tt.err})
			router := gin.New()
			router.POST("/auth/google", handler.Google)

			req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/auth/google", bytes.NewBufferString(`{"credential":"token"}`))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusConflict {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
			}
			var payload struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if payload.Code != tt.code {
				t.Fatalf("code = %q, want %q", payload.Code, tt.code)
			}
		})
	}
}
