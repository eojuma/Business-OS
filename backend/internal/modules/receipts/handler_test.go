package receipts

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/businessos/backend/internal/shared/middleware"
	"github.com/businessos/backend/internal/shared/permissions"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type fakeReceiptService struct {
	commits int
}

func (s *fakeReceiptService) Scan(context.Context, uuid.UUID, []byte, string) (*ScanResult, error) {
	return nil, nil
}

func (s *fakeReceiptService) Commit(_ uuid.UUID, input CommitInput) (*CommitResult, error) {
	s.commits++
	return &CommitResult{RecordType: input.RecordType, ID: uuid.New(), TotalAmount: 100}, nil
}

func TestCommitRequiresPermissionForRecordType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	productID := uuid.NewString()
	tests := []struct {
		name       string
		role       string
		recordType string
		wantStatus int
		wantCommit bool
	}{
		{"cashier can commit sale", permissions.RoleCashier, "sale", http.StatusCreated, true},
		{"cashier cannot commit purchase", permissions.RoleCashier, "purchase", http.StatusForbidden, false},
		{"warehouse cannot commit purchase", permissions.RoleWarehouse, "purchase", http.StatusForbidden, false},
		{"warehouse cannot commit sale", permissions.RoleWarehouse, "sale", http.StatusForbidden, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeReceiptService{}
			handler := NewHandler(service)
			router := gin.New()
			router.POST("/receipts/commit", func(c *gin.Context) {
				c.Set(middleware.ContextBusinessIDKey, uuid.NewString())
				c.Set(middleware.ContextRoleKey, tt.role)
				c.Next()
			}, handler.Commit)

			body := `{"record_type":"` + tt.recordType + `","line_items":[{"product_id":"` + productID + `","quantity":1}]}`
			request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/receipts/commit", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}
			if (service.commits > 0) != tt.wantCommit {
				t.Fatalf("commits = %d, want commit=%t", service.commits, tt.wantCommit)
			}
		})
	}
}
