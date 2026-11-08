package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/businessos/backend/internal/shared/database"
	"github.com/businessos/backend/internal/shared/middleware"
	"github.com/businessos/backend/internal/shared/migrations"
	"github.com/businessos/backend/internal/shared/response"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const testErrorKey = "error"

// setupAuditTestDB documents its scenario and asserts the expected audit behaviour.
func setupAuditTestDB(t *testing.T) (*gorm.DB, uuid.UUID, uuid.UUID) {
	t.Helper()
	testDBURL := os.Getenv("TEST_DATABASE_URL")
	if testDBURL == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}
	parsedURL, err := url.Parse(testDBURL)
	if err != nil || parsedURL.Hostname() == "" || parsedURL.User == nil {
		t.Fatalf("invalid TEST_DATABASE_URL: %v", err)
	}
	if dbName := strings.TrimPrefix(parsedURL.Path, "/"); dbName == "" {
		t.Fatal("invalid TEST_DATABASE_URL: database name is required")
	}

	db := database.NewPostgresFromURL(testDBURL)
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.WithContext(ctx).Exec("SELECT 1").Error; err != nil {
		t.Fatalf("database not reachable: %v", err)
	}

	schemaName := fmt.Sprintf("test_audit_%s", uuid.New().String()[:8])
	if err := db.Exec(fmt.Sprintf("CREATE SCHEMA %s", schemaName)).Error; err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	db = db.Session(&gorm.Session{NewDB: true})
	if err := db.Exec(fmt.Sprintf("SET search_path TO %s", schemaName)).Error; err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	if err := migrations.Up(db); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	businessID := uuid.New()
	userID := uuid.New()
	if err := db.Exec(
		"INSERT INTO businesses (id, name) VALUES (?, ?)",
		businessID, "Audit Test Business",
	).Error; err != nil {
		t.Fatalf("create business: %v", err)
	}
	if err := db.Exec(
		`INSERT INTO users (id, business_id, name, email, password_hash, role, active, email_verified)
		 VALUES (?, ?, ?, ?, ?, ?, TRUE, TRUE)`,
		userID, businessID, "Ada Auditor", "ada@example.com", "super-secret-hash", "owner",
	).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	t.Cleanup(func() {
		if err := db.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schemaName)).Error; err != nil {
			t.Logf("cleanup warning: %v", err)
		}
	})
	return db, businessID, userID
}

// auditRouter documents its scenario and asserts the expected audit behaviour.
func auditRouter(db *gorm.DB, businessID, userID uuid.UUID) *gin.Engine {
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(middleware.ContextUserIDKey, userID.String())
		c.Set(middleware.ContextBusinessIDKey, businessID.String())
		c.Next()
	}, Middleware(db))
	return router
}

// TestMiddlewareRecordsProductUpdate documents its scenario and asserts the expected audit behaviour.
func TestMiddlewareRecordsProductUpdate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, businessID, userID := setupAuditTestDB(t)
	productID := uuid.New()
	if err := db.Exec(
		`INSERT INTO products (id, business_id, name, unit, price) VALUES (?, ?, ?, ?, ?)`,
		productID, businessID, "Old Name", "unit", 100,
	).Error; err != nil {
		t.Fatalf("create product: %v", err)
	}

	router := auditRouter(db, businessID, userID)
	router.PATCH("/api/v1/products/:id", func(c *gin.Context) {
		var req struct {
			Name string `json:"name"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{testErrorKey: err.Error()})
			return
		}
		if err := db.Exec("UPDATE products SET name = ? WHERE id = ?", req.Name, productID).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{testErrorKey: err.Error()})
			return
		}
		var row map[string]any
		if err := db.Raw("SELECT * FROM products WHERE id = ?", productID).Scan(&row).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{testErrorKey: err.Error()})
			return
		}
		response.Success(c, http.StatusOK, row)
	})

	request := httptest.NewRequestWithContext(context.Background(), http.MethodPatch, "/api/v1/products/"+productID.String(), strings.NewReader(`{"name":"New Name"}`))
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = "192.0.2.10:1234"
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}

	var entry Entry
	if err := db.First(&entry).Error; err != nil {
		t.Fatalf("load audit entry: %v", err)
	}
	if entry.Action != ActionUpdate || entry.EntityType != testEntityProduct || entry.EntityID == nil || *entry.EntityID != productID {
		t.Fatalf("entry = %+v", entry)
	}
	if entry.ActorUserID == nil || *entry.ActorUserID != userID || entry.ActorName != "Ada Auditor" || entry.ActorEmail != "ada@example.com" {
		t.Fatalf("actor = %+v", entry)
	}
	if entry.IPAddress != "192.0.2.10" {
		t.Fatalf("ip = %q", entry.IPAddress)
	}
	var before, after map[string]any
	if err := json.Unmarshal(entry.Before, &before); err != nil {
		t.Fatalf("decode before: %v", err)
	}
	if err := json.Unmarshal(entry.After, &after); err != nil {
		t.Fatalf("decode after: %v", err)
	}
	if before["name"] != "Old Name" || after["name"] != "New Name" {
		t.Fatalf("before=%v after=%v", before["name"], after["name"])
	}
}

// TestMiddlewareRecordsRoleChangeWithoutSecrets documents its scenario and asserts the expected audit behaviour.
func TestMiddlewareRecordsRoleChangeWithoutSecrets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, businessID, userID := setupAuditTestDB(t)
	targetID := uuid.New()
	if err := db.Exec(
		`INSERT INTO users (id, business_id, name, email, password_hash, role, active, email_verified)
		 VALUES (?, ?, ?, ?, ?, ?, TRUE, TRUE)`,
		targetID, businessID, "Target User", "target@example.com", "target-secret-hash", "cashier",
	).Error; err != nil {
		t.Fatalf("create target: %v", err)
	}

	router := auditRouter(db, businessID, userID)
	router.PATCH("/api/v1/users/:id/role", func(c *gin.Context) {
		var req struct {
			Role string `json:"role"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{testErrorKey: err.Error()})
			return
		}
		if err := db.Exec("UPDATE users SET role = ? WHERE id = ?", req.Role, targetID).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{testErrorKey: err.Error()})
			return
		}
		response.Success(c, http.StatusOK, map[string]any{
			"id":            targetID,
			"role":          req.Role,
			"password_hash": "should-not-be-audited",
			"access_token":  "should-not-be-audited",
		})
	})

	request := httptest.NewRequestWithContext(context.Background(), http.MethodPatch, "/api/v1/users/"+targetID.String()+"/role", strings.NewReader(`{"role":"manager"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}

	var entry Entry
	if err := db.First(&entry).Error; err != nil {
		t.Fatalf("load audit entry: %v", err)
	}
	if entry.Action != ActionRoleChange || entry.EntityType != "user" || entry.EntityID == nil || *entry.EntityID != targetID {
		t.Fatalf("entry = %+v", entry)
	}
	before := string(entry.Before)
	after := string(entry.After)
	for _, forbidden := range []string{"password_hash", "target-secret-hash", "access_token"} {
		if strings.Contains(before, forbidden) || strings.Contains(after, forbidden) {
			t.Fatalf("audit entry leaked %q: before=%s after=%s", forbidden, before, after)
		}
	}
}

// TestMiddlewareClassifiesReceiptCommitByRecordType checks receipt entity resolution.
func TestMiddlewareClassifiesReceiptCommitByRecordType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, businessID, userID := setupAuditTestDB(t)
	saleID := uuid.New()

	router := auditRouter(db, businessID, userID)
	router.POST("/api/v1/receipts/commit", func(c *gin.Context) {
		response.Success(c, http.StatusCreated, map[string]any{
			"record_type":  "sale",
			"id":           saleID,
			"total_amount": 100,
		})
	})

	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/receipts/commit", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}

	var entry Entry
	if err := db.First(&entry).Error; err != nil {
		t.Fatalf("load audit entry: %v", err)
	}
	if entry.Action != ActionReceiptCommit || entry.EntityType != "sale" || entry.EntityID == nil || *entry.EntityID != saleID {
		t.Fatalf("entry = %+v", entry)
	}
}

// TestMiddlewareUsesHandlerSuppliedStockMovementID checks context overrides.
func TestMiddlewareUsesHandlerSuppliedStockMovementID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, businessID, userID := setupAuditTestDB(t)
	movementID := uuid.New()
	stockLevelID := uuid.New()

	router := auditRouter(db, businessID, userID)
	router.POST("/api/v1/inventory/movements", func(c *gin.Context) {
		SetEntityID(c, movementID)
		response.Success(c, http.StatusCreated, map[string]any{"id": stockLevelID, "quantity": 5})
	})

	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/inventory/movements", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}

	var entry Entry
	if err := db.First(&entry).Error; err != nil {
		t.Fatalf("load audit entry: %v", err)
	}
	if entry.Action != ActionStockMovement || entry.EntityType != "stock_movement" {
		t.Fatalf("entry = %+v", entry)
	}
	if entry.EntityID == nil || *entry.EntityID != movementID {
		t.Fatalf("entity id = %v, want movement %s", entry.EntityID, movementID)
	}
}
