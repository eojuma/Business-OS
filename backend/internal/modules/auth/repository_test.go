package auth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/businessos/backend/internal/config"
	"github.com/businessos/backend/internal/modules/business"
	"github.com/businessos/backend/internal/shared/database"
	"github.com/businessos/backend/internal/shared/migrations"
)

func setupAuthTestDB(t *testing.T) (*gorm.DB, uuid.UUID) {
	t.Helper()

	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}

	cfg := config.Load()
	cfg.DBName = "businessos_test"
	cfg.DBHost = "localhost"
	cfg.DBPort = "5432"
	cfg.DBUser = "businessos"
	cfg.DBPassword = "businessos"
	cfg.DBSSLMode = "disable"

	db := database.NewPostgres(cfg)
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.WithContext(ctx).Exec("SELECT 1").Error; err != nil {
		t.Skipf("Database not reachable, skipping integration test: %v", err)
	}

	schemaName := fmt.Sprintf("auth_test_%s", uuid.New().String()[:8])
	if err := db.Exec(fmt.Sprintf("CREATE SCHEMA %s", schemaName)).Error; err != nil {
		t.Fatalf("failed to create test schema: %v", err)
	}
	if err := db.Exec(fmt.Sprintf("SET search_path TO %s", schemaName)).Error; err != nil {
		t.Fatalf("failed to set search_path: %v", err)
	}
	if err := migrations.Up(db); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	testBusiness := business.Business{ID: uuid.New(), Name: "Auth Test Business"}
	if err := db.Create(&testBusiness).Error; err != nil {
		t.Fatalf("failed to create test business: %v", err)
	}

	t.Cleanup(func() {
		if err := db.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schemaName)).Error; err != nil {
			t.Logf("cleanup warning: failed to drop test schema %s: %v", schemaName, err)
		}
	})

	return db, testBusiness.ID
}

func TestRepositoryGoogleIdentity(t *testing.T) {
	db, businessID := setupAuthTestDB(t)
	repo := NewRepository(db)

	googleID := "google-sub-db-1"
	user := &User{BusinessID: businessID, Name: "Google User", Email: "google-db@example.com", GoogleID: &googleID}
	if err := repo.Create(user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	found, err := repo.FindByGoogleID(googleID)
	if err != nil {
		t.Fatalf("FindByGoogleID failed: %v", err)
	}
	if found.ID != user.ID || found.Email != user.Email {
		t.Fatalf("found user = %+v, want id=%v email=%q", found, user.ID, user.Email)
	}
	if found.PasswordHash != nil {
		t.Fatal("expected password_hash to be NULL for a google-only user")
	}
}

func TestRepositorySetGoogleID(t *testing.T) {
	db, businessID := setupAuthTestDB(t)
	repo := NewRepository(db)

	user := &User{BusinessID: businessID, Name: "Link Me", Email: "link-db@example.com"}
	if err := repo.Create(user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if err := repo.SetGoogleID(user.ID, "google-sub-db-2"); err != nil {
		t.Fatalf("SetGoogleID failed: %v", err)
	}

	found, err := repo.FindByGoogleID("google-sub-db-2")
	if err != nil {
		t.Fatalf("FindByGoogleID failed: %v", err)
	}
	if found.ID != user.ID {
		t.Fatalf("found id = %v, want %v", found.ID, user.ID)
	}

	if err := repo.SetGoogleID(uuid.New(), "google-sub-db-3"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("SetGoogleID error = %v, want %v", err, gorm.ErrRecordNotFound)
	}
}
