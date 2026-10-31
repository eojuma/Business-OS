package auth

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
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

	testDBURL := os.Getenv("TEST_DATABASE_URL")
	if testDBURL == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}

	parsedURL, err := url.Parse(testDBURL)
	if err != nil || parsedURL.Hostname() == "" || parsedURL.User == nil {
		t.Fatalf("invalid TEST_DATABASE_URL: %v", err)
	}

	cfg := config.Load()
	cfg.DBHost = parsedURL.Hostname()
	cfg.DBPort = "5432" // PostgreSQL default when the URL omits a port
	if port := parsedURL.Port(); port != "" {
		cfg.DBPort = port
	}
	cfg.DBUser = parsedURL.User.Username()
	cfg.DBPassword = ""
	if password, ok := parsedURL.User.Password(); ok {
		cfg.DBPassword = password
	}
	if dbName := strings.TrimPrefix(parsedURL.Path, "/"); dbName != "" {
		cfg.DBName = dbName
	}
	if sslMode := parsedURL.Query().Get("sslmode"); sslMode != "" {
		cfg.DBSSLMode = sslMode
	}

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

func TestBusinessOwnerCreatorCreatesWorkspaceAndOwner(t *testing.T) {
	db, _ := setupAuthTestDB(t)
	creator := &businessOwnerCreator{db: db}

	googleID := "google-sub-workspace"
	user := &User{Name: "Workspace Owner", Email: "workspace-owner@example.com", GoogleID: &googleID, Role: testRoleOwner, Active: true}
	if err := creator.CreateWithOwner("Workspace Store", "0700", user); err != nil {
		t.Fatalf("CreateWithOwner failed: %v", err)
	}
	if user.ID == uuid.Nil || user.BusinessID == uuid.Nil {
		t.Fatal("expected the user and business ids to be populated")
	}

	var createdBusiness business.Business
	if err := db.First(&createdBusiness, "id = ?", user.BusinessID).Error; err != nil {
		t.Fatalf("find business: %v", err)
	}
	if createdBusiness.Name != "Workspace Store" || createdBusiness.Phone != "0700" {
		t.Fatalf("business = %+v, want name=Workspace Store phone=0700", createdBusiness)
	}
	if createdBusiness.OwnerUserID == nil || *createdBusiness.OwnerUserID != user.ID {
		t.Fatalf("owner_user_id = %v, want %v", createdBusiness.OwnerUserID, user.ID)
	}

	var storedUser User
	if err := db.First(&storedUser, "id = ?", user.ID).Error; err != nil {
		t.Fatalf("find user: %v", err)
	}
	if storedUser.PasswordHash != nil {
		t.Fatal("expected no password hash for a google-only owner")
	}
	if storedUser.GoogleID == nil || *storedUser.GoogleID != googleID {
		t.Fatalf("google_id = %v, want %q", storedUser.GoogleID, googleID)
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

func TestRepositoryFindByEmailCaseInsensitive(t *testing.T) {
	db, businessID := setupAuthTestDB(t)
	repo := NewRepository(db)

	user := &User{BusinessID: businessID, Name: "Case User", Email: "Case.User@Example.com", Role: testRoleOwner, Active: true}
	if err := repo.Create(user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	found, err := repo.FindByEmail("case.user@example.com")
	if err != nil {
		t.Fatalf("FindByEmail failed: %v", err)
	}
	if found.ID != user.ID {
		t.Fatalf("found user %v, want %v", found.ID, user.ID)
	}
}

func TestRepositoryCreatePersistsUnverifiedFalse(t *testing.T) {
	db, businessID := setupAuthTestDB(t)
	repo := NewRepository(db)

	user := &User{BusinessID: businessID, Name: "Unverified", Email: "unverified-db@example.com", EmailVerified: false}
	if err := repo.Create(user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	var stored User
	if err := db.First(&stored, "id = ?", user.ID).Error; err != nil {
		t.Fatalf("find user: %v", err)
	}
	if stored.EmailVerified {
		t.Fatal("expected email_verified to stay false for a new sign-up")
	}

	if err := repo.MarkEmailVerified(user.ID); err != nil {
		t.Fatalf("MarkEmailVerified failed: %v", err)
	}
	if err := db.First(&stored, "id = ?", user.ID).Error; err != nil {
		t.Fatalf("find user after verify: %v", err)
	}
	if !stored.EmailVerified {
		t.Fatal("expected email_verified to become true")
	}
}

func TestRepositoryEmailVerificationTokenLifecycle(t *testing.T) {
	db, businessID := setupAuthTestDB(t)
	repo := NewRepository(db)

	user := &User{BusinessID: businessID, Name: "Token User", Email: "token-db@example.com"}
	if err := repo.Create(user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	raw := "integration-token"
	token := &EmailVerificationToken{UserID: user.ID, TokenHash: hashToken(raw), ExpiresAt: time.Now().Add(time.Hour)}
	if err := repo.CreateEmailVerificationToken(token); err != nil {
		t.Fatalf("CreateEmailVerificationToken failed: %v", err)
	}

	found, err := repo.FindEmailVerificationTokenByHash(hashToken(raw))
	if err != nil {
		t.Fatalf("FindEmailVerificationTokenByHash failed: %v", err)
	}
	if found.UserID != user.ID {
		t.Fatalf("token user = %v, want %v", found.UserID, user.ID)
	}

	consumed, err := repo.ConsumeEmailVerificationToken(found.ID)
	if err != nil {
		t.Fatalf("ConsumeEmailVerificationToken failed: %v", err)
	}
	if !consumed {
		t.Fatal("expected the first consume to succeed")
	}
	again, err := repo.ConsumeEmailVerificationToken(found.ID)
	if err != nil {
		t.Fatalf("second ConsumeEmailVerificationToken failed: %v", err)
	}
	if again {
		t.Fatal("a token must not be consumable twice")
	}

	if err := repo.DeleteEmailVerificationTokensForUser(user.ID); err != nil {
		t.Fatalf("DeleteEmailVerificationTokensForUser failed: %v", err)
	}
	if _, err := repo.FindEmailVerificationTokenByHash(hashToken(raw)); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("token should be gone, got %v", err)
	}
}
