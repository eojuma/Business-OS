package auth

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/businessos/backend/internal/config"
	"github.com/businessos/backend/internal/shared/email"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/api/idtoken"
	"gorm.io/gorm"
)

const (
	testCredential = "token"
	testRoleOwner  = "owner"
)

type fakeRepository struct {
	users   []*User
	tokens  []*EmailVerificationToken
	created *User
	setID   uuid.UUID
	setSub  string
}

func (r *fakeRepository) Create(user *User) error {
	if user.ID == uuid.Nil {
		user.ID = uuid.New()
	}
	r.created = user
	r.users = append(r.users, user)
	return nil
}

func (r *fakeRepository) FindByEmail(email string) (*User, error) {
	for _, user := range r.users {
		if strings.EqualFold(user.Email, email) {
			return user, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (r *fakeRepository) FindByGoogleID(googleID string) (*User, error) {
	for _, user := range r.users {
		if user.GoogleID != nil && *user.GoogleID == googleID {
			return user, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (r *fakeRepository) FindByID(id uuid.UUID) (*User, error) {
	for _, user := range r.users {
		if user.ID == id {
			return user, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (r *fakeRepository) ListByBusiness(businessID uuid.UUID) ([]User, error) {
	users := make([]User, 0, len(r.users))
	for _, user := range r.users {
		if user.BusinessID == businessID {
			users = append(users, *user)
		}
	}
	return users, nil
}

func (r *fakeRepository) SetGoogleID(id uuid.UUID, googleID string) error {
	for _, user := range r.users {
		if user.ID == id {
			sub := googleID
			user.GoogleID = &sub
			r.setID = id
			r.setSub = googleID
			return nil
		}
	}
	return gorm.ErrRecordNotFound
}

func (r *fakeRepository) ClearGoogleID(id uuid.UUID) error {
	for _, user := range r.users {
		if user.ID == id {
			user.GoogleID = nil
			return nil
		}
	}
	return gorm.ErrRecordNotFound
}

func (r *fakeRepository) ClearPassword(id uuid.UUID) error {
	for _, user := range r.users {
		if user.ID == id {
			user.PasswordHash = nil
			return nil
		}
	}
	return gorm.ErrRecordNotFound
}

func (r *fakeRepository) MarkEmailVerified(id uuid.UUID) error {
	for _, user := range r.users {
		if user.ID == id {
			user.EmailVerified = true
			return nil
		}
	}
	return gorm.ErrRecordNotFound
}

func (r *fakeRepository) CreateEmailVerificationToken(token *EmailVerificationToken) error {
	if token.ID == uuid.Nil {
		token.ID = uuid.New()
	}
	r.tokens = append(r.tokens, token)
	return nil
}

func (r *fakeRepository) FindEmailVerificationTokenByHash(hash string) (*EmailVerificationToken, error) {
	for _, token := range r.tokens {
		if token.TokenHash == hash {
			return token, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (r *fakeRepository) ConsumeEmailVerificationToken(id uuid.UUID) (bool, error) {
	for _, token := range r.tokens {
		if token.ID == id {
			if token.UsedAt != nil {
				return false, nil
			}
			now := time.Now()
			token.UsedAt = &now
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeRepository) DeleteEmailVerificationTokensForUser(userID uuid.UUID) error {
	kept := r.tokens[:0]
	for _, token := range r.tokens {
		if token.UserID != userID {
			kept = append(kept, token)
		}
	}
	r.tokens = kept
	return nil
}

type fakeVerifier struct {
	identity *GoogleIdentity
	err      error
}

func (v *fakeVerifier) Verify(context.Context, string) (*GoogleIdentity, error) {
	return v.identity, v.err
}

type fakeBusinessChecker struct {
	exists bool
	err    error
}

func (c *fakeBusinessChecker) Exists(uuid.UUID) (bool, error) {
	return c.exists, c.err
}

type fakeBusinessCreator struct {
	repo       *fakeRepository
	businessID uuid.UUID
	err        error
	calls      int
	name       string
	phone      string
}

func (c *fakeBusinessCreator) CreateWithOwner(name, phone string, user *User) error {
	c.calls++
	c.name, c.phone = name, phone
	if c.err != nil {
		return c.err
	}
	if c.businessID == uuid.Nil {
		c.businessID = uuid.New()
	}
	user.BusinessID = c.businessID
	if user.ID == uuid.Nil {
		user.ID = uuid.New()
	}
	if c.repo != nil {
		return c.repo.Create(user)
	}
	return nil
}

func testService(repo Repository, google GoogleTokenVerifier, creator BusinessCreator) Service {
	return NewService(
		repo,
		&config.Config{JWTSecret: "test-secret", JWTExpiryHours: 1, GoogleClientID: "client-id"},
		&fakeBusinessChecker{exists: true},
		google,
		creator,
		nil,
	)
}

func googleIdentity(sub, email string, verified bool) *GoogleIdentity {
	return &GoogleIdentity{Subject: sub, Email: email, EmailVerified: verified, Name: "Test User"}
}

func TestGoogleLoginExistingUser(t *testing.T) {
	sub := "google-sub-1"
	user := &User{ID: uuid.New(), BusinessID: uuid.New(), Name: "Existing", Email: "existing@example.com", GoogleID: &sub, Role: testRoleOwner, Active: true}
	repo := &fakeRepository{users: []*User{user}}
	creator := &fakeBusinessCreator{}
	svc := testService(repo, &fakeVerifier{identity: googleIdentity(sub, user.Email, true)}, creator)

	result, err := svc.GoogleLogin(context.Background(), GoogleLoginInput{Credential: testCredential})
	if err != nil {
		t.Fatalf("GoogleLogin failed: %v", err)
	}
	if result.Token == "" {
		t.Fatal("expected a token")
	}
	if result.User != user {
		t.Fatal("expected the existing user")
	}
	if creator.calls != 0 {
		t.Fatal("a workspace should not be created for an existing user")
	}
}

func TestGoogleLoginRejectsLinkingPasswordAccount(t *testing.T) {
	hash := "existing-hash"
	user := &User{ID: uuid.New(), BusinessID: uuid.New(), Name: "Password User", Email: "link@example.com", PasswordHash: &hash, Role: testRoleOwner, Active: true, EmailVerified: true}
	repo := &fakeRepository{users: []*User{user}}
	svc := testService(repo, &fakeVerifier{identity: googleIdentity("google-sub-2", user.Email, true)}, nil)

	if _, err := svc.GoogleLogin(context.Background(), GoogleLoginInput{Credential: testCredential}); err != ErrGoogleLinkRequiresPassword {
		t.Fatalf("error = %v, want %v", err, ErrGoogleLinkRequiresPassword)
	}
	if repo.setID != uuid.Nil || user.GoogleID != nil {
		t.Fatal("a password account must not be linked to a google identity")
	}
}

func TestGoogleLoginLinksPasswordlessEmailAccount(t *testing.T) {
	user := &User{ID: uuid.New(), BusinessID: uuid.New(), Name: "Google User", Email: "link@example.com", Active: true}
	repo := &fakeRepository{users: []*User{user}}
	svc := testService(repo, &fakeVerifier{identity: googleIdentity("google-sub-2", user.Email, true)}, nil)

	result, err := svc.GoogleLogin(context.Background(), GoogleLoginInput{Credential: testCredential})
	if err != nil {
		t.Fatalf("GoogleLogin failed: %v", err)
	}
	if repo.setID != user.ID || repo.setSub != "google-sub-2" {
		t.Fatalf("expected google id to be linked, got id=%v sub=%q", repo.setID, repo.setSub)
	}
	if result.User.GoogleID == nil || *result.User.GoogleID != "google-sub-2" {
		t.Fatal("expected the user to carry the linked google id")
	}
}

func TestGoogleLoginRejectsDifferentGoogleAccount(t *testing.T) {
	other := "google-sub-other"
	user := &User{ID: uuid.New(), BusinessID: uuid.New(), Email: "conflict@example.com", GoogleID: &other, Active: true}
	repo := &fakeRepository{users: []*User{user}}
	svc := testService(repo, &fakeVerifier{identity: googleIdentity("google-sub-new", user.Email, true)}, nil)

	if _, err := svc.GoogleLogin(context.Background(), GoogleLoginInput{Credential: testCredential}); err != ErrGoogleAccountConflict {
		t.Fatalf("error = %v, want %v", err, ErrGoogleAccountConflict)
	}
}

func TestGoogleLoginRequiresWorkspaceForNewUser(t *testing.T) {
	repo := &fakeRepository{}
	creator := &fakeBusinessCreator{}
	svc := testService(repo, &fakeVerifier{identity: googleIdentity("google-sub-3", "new@example.com", true)}, creator)

	if _, err := svc.GoogleLogin(context.Background(), GoogleLoginInput{Credential: testCredential}); err != ErrWorkspaceRequired {
		t.Fatalf("error = %v, want %v", err, ErrWorkspaceRequired)
	}
	if creator.calls != 0 {
		t.Fatal("a workspace should not be created without a business name")
	}
}

func TestGoogleLoginCreatesWorkspaceForNewUser(t *testing.T) {
	repo := &fakeRepository{}
	creator := &fakeBusinessCreator{repo: repo}
	svc := testService(repo, &fakeVerifier{identity: googleIdentity("google-sub-4", "new@example.com", true)}, creator)

	result, err := svc.GoogleLogin(context.Background(), GoogleLoginInput{
		Credential:    "token",
		BusinessName:  "New Store",
		BusinessPhone: "0700",
	})
	if err != nil {
		t.Fatalf("GoogleLogin failed: %v", err)
	}
	if creator.calls != 1 || creator.name != "New Store" || creator.phone != "0700" {
		t.Fatalf("creator called %d times with name=%q phone=%q", creator.calls, creator.name, creator.phone)
	}
	if result.User.BusinessID != creator.businessID {
		t.Fatal("expected the user to be attached to the created business")
	}
	if result.User.PasswordHash != nil {
		t.Fatal("google-only users must not have a password hash")
	}
	if result.User.GoogleID == nil || *result.User.GoogleID != "google-sub-4" {
		t.Fatal("expected the google id to be stored")
	}
	if repo.created != result.User {
		t.Fatal("expected the user to be persisted")
	}
}

func TestGoogleLoginRejectsUnverifiedEmail(t *testing.T) {
	svc := testService(&fakeRepository{}, &fakeVerifier{identity: googleIdentity("google-sub-5", "unverified@example.com", false)}, nil)
	if _, err := svc.GoogleLogin(context.Background(), GoogleLoginInput{Credential: testCredential, BusinessName: "Store"}); err != ErrUnverifiedGoogleEmail {
		t.Fatalf("error = %v, want %v", err, ErrUnverifiedGoogleEmail)
	}
}

func TestGoogleLoginRejectsInactiveUser(t *testing.T) {
	sub := "google-sub-6"
	user := &User{ID: uuid.New(), BusinessID: uuid.New(), Email: "inactive@example.com", GoogleID: &sub, Active: false}
	svc := testService(&fakeRepository{users: []*User{user}}, &fakeVerifier{identity: googleIdentity(sub, user.Email, true)}, nil)

	if _, err := svc.GoogleLogin(context.Background(), GoogleLoginInput{Credential: testCredential}); err != ErrInactiveUser {
		t.Fatalf("error = %v, want %v", err, ErrInactiveUser)
	}
}

func TestGoogleLoginRejectsInvalidToken(t *testing.T) {
	svc := testService(&fakeRepository{}, &fakeVerifier{err: gorm.ErrRecordNotFound}, nil)
	if _, err := svc.GoogleLogin(context.Background(), GoogleLoginInput{Credential: "bad"}); err != ErrInvalidGoogleToken {
		t.Fatalf("error = %v, want %v", err, ErrInvalidGoogleToken)
	}
}

func TestGoogleLoginUnavailableWhenNotConfigured(t *testing.T) {
	svc := NewService(&fakeRepository{}, &config.Config{JWTSecret: "test-secret", JWTExpiryHours: 1}, &fakeBusinessChecker{}, nil, nil, nil)
	if _, err := svc.GoogleLogin(context.Background(), GoogleLoginInput{Credential: testCredential}); err != ErrGoogleAuthUnavailable {
		t.Fatalf("error = %v, want %v", err, ErrGoogleAuthUnavailable)
	}
}

func TestLoginRejectsGoogleOnlyAccountWithoutDisclosure(t *testing.T) {
	sub := "google-sub-7"
	user := &User{ID: uuid.New(), BusinessID: uuid.New(), Email: "googleonly@example.com", GoogleID: &sub, Active: true}
	svc := testService(&fakeRepository{users: []*User{user}}, nil, nil)

	// Same error as an unknown address, so callers cannot enumerate accounts.
	if _, err := svc.Login(LoginInput{Email: user.Email, Password: "anything"}); err != ErrInvalidCredentials {
		t.Fatalf("error = %v, want %v", err, ErrInvalidCredentials)
	}
}

func TestLoginStillAcceptsPasswordAccount(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	hashStr := string(hash)
	user := &User{ID: uuid.New(), BusinessID: uuid.New(), Email: "password@example.com", PasswordHash: &hashStr, Role: testRoleOwner, Active: true, EmailVerified: true}
	svc := testService(&fakeRepository{users: []*User{user}}, nil, nil)

	result, err := svc.Login(LoginInput{Email: user.Email, Password: "password123"})
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if result.Token == "" {
		t.Fatal("expected a token")
	}
}

func TestNewGoogleTokenVerifierDisabledWithoutClientID(t *testing.T) {
	verifier, err := NewGoogleTokenVerifier(context.Background(), "")
	if err != nil {
		t.Fatalf("NewGoogleTokenVerifier failed: %v", err)
	}
	if verifier != nil {
		t.Fatal("expected no verifier when the client id is empty")
	}
}

func TestNewGoogleTokenVerifierConfigured(t *testing.T) {
	verifier, err := NewGoogleTokenVerifier(context.Background(), "client-id")
	if err != nil {
		t.Fatalf("NewGoogleTokenVerifier failed: %v", err)
	}
	if verifier == nil {
		t.Fatal("expected a verifier when the client id is set")
	}
}

func TestGoogleTokenVerifierMapsClaims(t *testing.T) {
	verifier := &idTokenVerifier{
		audience: "client-id",
		validate: func(context.Context, string, string) (*idtoken.Payload, error) {
			return &idtoken.Payload{
				Issuer:  "accounts.google.com",
				Subject: "subject-1",
				Claims: map[string]interface{}{
					"email":          "person@example.com",
					"email_verified": true,
					"name":           "Person",
				},
			}, nil
		},
	}

	identity, err := verifier.Verify(context.Background(), "credential")
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
	if identity.Subject != "subject-1" || identity.Email != "person@example.com" || !identity.EmailVerified || identity.Name != "Person" {
		t.Fatalf("unexpected identity: %+v", identity)
	}
}

func TestGoogleTokenVerifierRejectsUnexpectedIssuer(t *testing.T) {
	verifier := &idTokenVerifier{
		audience: "client-id",
		validate: func(context.Context, string, string) (*idtoken.Payload, error) {
			return &idtoken.Payload{
				Issuer:  "accounts.example.com",
				Subject: "subject-1",
				Claims: map[string]interface{}{
					"email":          "person@example.com",
					"email_verified": true,
				},
			}, nil
		},
	}

	if _, err := verifier.Verify(context.Background(), "credential"); err == nil {
		t.Fatal("expected a non-Google issuer to be rejected")
	}
}

func TestRegisterNormalizesEmail(t *testing.T) {
	repo := &fakeRepository{}
	svc := NewService(repo, &config.Config{JWTSecret: "test-secret", JWTExpiryHours: 1}, &fakeBusinessChecker{exists: true}, nil, nil, nil)

	result, err := svc.Register(RegisterInput{BusinessID: uuid.New(), Name: "John", Email: " John@Example.COM ", Password: "secret"})
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if result.Email != "john@example.com" {
		t.Fatalf("email = %q, want %q", result.Email, "john@example.com")
	}
	if repo.created == nil || repo.created.Email != "john@example.com" {
		t.Fatalf("stored email was not normalized: %+v", repo.created)
	}
}

func TestGoogleLoginMatchesMixedCaseEmail(t *testing.T) {
	user := &User{ID: uuid.New(), BusinessID: uuid.New(), Name: "Mixed Case", Email: "John@Example.com", Active: true}
	repo := &fakeRepository{users: []*User{user}}
	svc := testService(repo, &fakeVerifier{identity: googleIdentity("google-sub-case", "john@example.com", true)}, nil)

	result, err := svc.GoogleLogin(context.Background(), GoogleLoginInput{Credential: testCredential})
	if err != nil {
		t.Fatalf("GoogleLogin failed: %v", err)
	}
	if result.User != user {
		t.Fatal("expected the existing mixed-case account to be reused")
	}
	if repo.setID != user.ID || repo.setSub != "google-sub-case" {
		t.Fatalf("expected the google id to be linked to the existing user, got id=%v sub=%q", repo.setID, repo.setSub)
	}
}

type fakeMailer struct {
	sent []email.Message
	err  error
}

func (m *fakeMailer) Send(_ context.Context, msg email.Message) error {
	if m.err != nil {
		return m.err
	}
	m.sent = append(m.sent, msg)
	return nil
}

func testServiceWithMailer(repo Repository, google GoogleTokenVerifier, creator BusinessCreator, mailer email.Sender) Service {
	return NewService(
		repo,
		&config.Config{JWTSecret: "test-secret", JWTExpiryHours: 1, GoogleClientID: "client-id", EmailVerificationTTLHours: 24, FrontendURL: "http://localhost:3000"},
		&fakeBusinessChecker{exists: true},
		google,
		creator,
		mailer,
	)
}

func TestRegisterCreatesUnverifiedUserAndSendsEmail(t *testing.T) {
	repo := &fakeRepository{}
	mailer := &fakeMailer{}
	svc := testServiceWithMailer(repo, nil, nil, mailer)

	user, err := svc.Register(RegisterInput{BusinessID: uuid.New(), Name: "John", Email: "john@example.com", Password: "secret123"})
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if user.EmailVerified {
		t.Fatal("new accounts must start unverified")
	}
	if len(repo.tokens) != 1 {
		t.Fatalf("verification tokens = %d, want 1", len(repo.tokens))
	}
	if len(mailer.sent) != 1 || mailer.sent[0].To != "john@example.com" {
		t.Fatalf("expected one verification email, got %+v", mailer.sent)
	}
}

func TestLoginRejectsUnverifiedAccount(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	hashStr := string(hash)
	user := &User{ID: uuid.New(), BusinessID: uuid.New(), Email: "pending@example.com", PasswordHash: &hashStr, Active: true, EmailVerified: false}
	svc := testService(&fakeRepository{users: []*User{user}}, nil, nil)

	if _, err := svc.Login(LoginInput{Email: user.Email, Password: "password123"}); err != ErrEmailNotVerified {
		t.Fatalf("error = %v, want %v", err, ErrEmailNotVerified)
	}
}

func TestVerifyEmailConsumesSingleUseToken(t *testing.T) {
	user := &User{ID: uuid.New(), BusinessID: uuid.New(), Email: "verify@example.com", Active: true, EmailVerified: false}
	raw := "one-time-token"
	repo := &fakeRepository{
		users: []*User{user},
		tokens: []*EmailVerificationToken{{
			ID:        uuid.New(),
			UserID:    user.ID,
			TokenHash: hashToken(raw),
			ExpiresAt: time.Now().Add(time.Hour),
		}},
	}
	svc := testService(repo, nil, nil)

	if err := svc.VerifyEmail(raw); err != nil {
		t.Fatalf("VerifyEmail failed: %v", err)
	}
	if !user.EmailVerified {
		t.Fatal("expected the account to be verified")
	}
	if err := svc.VerifyEmail(raw); err != ErrInvalidVerificationToken {
		t.Fatalf("replay error = %v, want %v", err, ErrInvalidVerificationToken)
	}
}

func TestVerifyEmailRejectsExpiredToken(t *testing.T) {
	user := &User{ID: uuid.New(), BusinessID: uuid.New(), Email: "expired@example.com", Active: true}
	raw := "expired-token"
	repo := &fakeRepository{
		users: []*User{user},
		tokens: []*EmailVerificationToken{{
			ID:        uuid.New(),
			UserID:    user.ID,
			TokenHash: hashToken(raw),
			ExpiresAt: time.Now().Add(-time.Minute),
		}},
	}
	svc := testService(repo, nil, nil)

	if err := svc.VerifyEmail(raw); err != ErrExpiredVerificationToken {
		t.Fatalf("error = %v, want %v", err, ErrExpiredVerificationToken)
	}
}

func TestResendVerificationIsGeneric(t *testing.T) {
	svc := testService(&fakeRepository{}, nil, nil)
	if err := svc.ResendVerification("missing@example.com"); err != nil {
		t.Fatalf("ResendVerification should not reveal missing accounts: %v", err)
	}
}

func TestGoogleLoginClaimsUnverifiedAccountAndClearsPassword(t *testing.T) {
	hash := "attacker-hash"
	user := &User{ID: uuid.New(), BusinessID: uuid.New(), Name: "Pre-registered", Email: "victim@example.com", PasswordHash: &hash, Active: true, EmailVerified: false}
	repo := &fakeRepository{users: []*User{user}}
	svc := testService(repo, &fakeVerifier{identity: googleIdentity("google-sub-claim", user.Email, true)}, nil)

	result, err := svc.GoogleLogin(context.Background(), GoogleLoginInput{Credential: testCredential})
	if err != nil {
		t.Fatalf("GoogleLogin failed: %v", err)
	}
	if !result.User.EmailVerified {
		t.Fatal("claimed account should be verified")
	}
	if result.User.PasswordHash != nil {
		t.Fatal("the untrusted password must be cleared on claim")
	}
	if result.User.GoogleID == nil || *result.User.GoogleID != "google-sub-claim" {
		t.Fatal("expected the google subject to be linked")
	}
}

func TestGoogleLoginVerifiedPasswordAccountRequiresLink(t *testing.T) {
	hash := "existing-hash"
	user := &User{ID: uuid.New(), BusinessID: uuid.New(), Email: "verified@example.com", PasswordHash: &hash, Active: true, EmailVerified: true}
	repo := &fakeRepository{users: []*User{user}}
	svc := testService(repo, &fakeVerifier{identity: googleIdentity("google-sub-v", user.Email, true)}, nil)

	if _, err := svc.GoogleLogin(context.Background(), GoogleLoginInput{Credential: testCredential}); err != ErrGoogleLinkRequiresPassword {
		t.Fatalf("error = %v, want %v", err, ErrGoogleLinkRequiresPassword)
	}
}

func TestLinkGoogleRequiresMatchingEmail(t *testing.T) {
	user := &User{ID: uuid.New(), BusinessID: uuid.New(), Email: "owner@example.com", Active: true, EmailVerified: true}
	repo := &fakeRepository{users: []*User{user}}
	svc := testService(repo, &fakeVerifier{identity: googleIdentity("google-sub-x", "other@example.com", true)}, nil)

	if _, err := svc.LinkGoogle(context.Background(), user.ID, testCredential); err != ErrGoogleEmailMismatch {
		t.Fatalf("error = %v, want %v", err, ErrGoogleEmailMismatch)
	}
	if user.GoogleID != nil {
		t.Fatal("google id must not be linked on email mismatch")
	}
}

func TestLinkGoogleSuccess(t *testing.T) {
	user := &User{ID: uuid.New(), BusinessID: uuid.New(), Email: "owner@example.com", Active: true, EmailVerified: true}
	repo := &fakeRepository{users: []*User{user}}
	svc := testService(repo, &fakeVerifier{identity: googleIdentity("google-sub-ok", user.Email, true)}, nil)

	linked, err := svc.LinkGoogle(context.Background(), user.ID, testCredential)
	if err != nil {
		t.Fatalf("LinkGoogle failed: %v", err)
	}
	if linked.GoogleID == nil || *linked.GoogleID != "google-sub-ok" {
		t.Fatal("expected the google subject to be linked")
	}
}

func TestUnlinkGoogleRefusesOnlyLogin(t *testing.T) {
	sub := "google-sub-only"
	user := &User{ID: uuid.New(), BusinessID: uuid.New(), Email: "googleonly2@example.com", GoogleID: &sub, Active: true, EmailVerified: true}
	svc := testService(&fakeRepository{users: []*User{user}}, nil, nil)

	if _, err := svc.UnlinkGoogle(user.ID); err != ErrCannotUnlinkGoogleOnly {
		t.Fatalf("error = %v, want %v", err, ErrCannotUnlinkGoogleOnly)
	}
}

func TestUnlinkGoogleWithPassword(t *testing.T) {
	hash := "hash"
	sub := "google-sub-linked"
	user := &User{ID: uuid.New(), BusinessID: uuid.New(), Email: "both@example.com", PasswordHash: &hash, GoogleID: &sub, Active: true, EmailVerified: true}
	svc := testService(&fakeRepository{users: []*User{user}}, nil, nil)

	updated, err := svc.UnlinkGoogle(user.ID)
	if err != nil {
		t.Fatalf("UnlinkGoogle failed: %v", err)
	}
	if updated.GoogleID != nil {
		t.Fatal("expected google id to be cleared")
	}
}

func TestRegisterCreatesWorkspaceInOneRequest(t *testing.T) {
	repo := &fakeRepository{}
	creator := &fakeBusinessCreator{repo: repo}
	mailer := &fakeMailer{}
	svc := testServiceWithMailer(repo, nil, creator, mailer)

	user, err := svc.Register(RegisterInput{
		BusinessName:  "New Store",
		BusinessPhone: "0700",
		Name:          "Owner",
		Email:         "owner@example.com",
		Password:      "secret123",
	})
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if creator.calls != 1 || creator.name != "New Store" || creator.phone != "0700" {
		t.Fatalf("creator called %d times name=%q phone=%q", creator.calls, creator.name, creator.phone)
	}
	if user.BusinessID != creator.businessID {
		t.Fatal("expected the user to be attached to the created business")
	}
	if user.EmailVerified {
		t.Fatal("new sign-ups must stay unverified until the email is confirmed")
	}
	if len(mailer.sent) != 1 {
		t.Fatalf("verification emails = %d, want 1", len(mailer.sent))
	}
}
