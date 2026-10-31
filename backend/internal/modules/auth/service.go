package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/businessos/backend/internal/config"
	"github.com/businessos/backend/internal/shared/email"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

var (
	ErrInvalidCredentials         = errors.New("invalid email or password")
	ErrEmailTaken                 = errors.New("email already registered")
	ErrBusinessNotFound           = errors.New("business does not exist")
	ErrInvalidRole                = errors.New("role must be owner, manager, or cashier")
	ErrGoogleAuthUnavailable      = errors.New("google sign-in is not configured")
	ErrInvalidGoogleToken         = errors.New("invalid google credential")
	ErrUnverifiedGoogleEmail      = errors.New("google account email is not verified")
	ErrGoogleAccountConflict      = errors.New("this email is linked to a different google account")
	ErrGoogleLinkRequiresPassword = errors.New("this email already has a password; sign in with your password, then connect Google from settings")
	ErrGoogleLinkRequired         = errors.New("sign in with your password, then connect Google from settings")
	ErrGoogleEmailMismatch        = errors.New("the google account email does not match your account email")
	ErrCannotUnlinkGoogleOnly     = errors.New("set a password before disconnecting Google")
	ErrWorkspaceRequired          = errors.New("no workspace is linked to this google account; create one to continue")
	ErrInactiveUser               = errors.New("user account is inactive")

	ErrEmailNotVerified         = errors.New("verify your email address before signing in")
	ErrInvalidVerificationToken = errors.New("this verification link is invalid or has already been used")
	ErrExpiredVerificationToken = errors.New("this verification link has expired")
)

// dummyPasswordHash is compared when an account has no local password, so a
// login attempt takes the same time whether the address belongs to a
// Google-only account or does not exist at all. Together with the generic
// credentials error this prevents account enumeration.
var dummyPasswordHash, _ = bcrypt.GenerateFromPassword([]byte("businessos-dummy-password"), bcrypt.DefaultCost)

type BusinessChecker interface {
	Exists(id uuid.UUID) (bool, error)
}

// BusinessCreator creates a business and its owner in one transaction.
type BusinessCreator interface {
	CreateWithOwner(name, phone string, user *User) error
}

type GoogleIdentity struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
}

// GoogleTokenVerifier verifies a Google ID token and returns its identity.
type GoogleTokenVerifier interface {
	Verify(ctx context.Context, credential string) (*GoogleIdentity, error)
}

type RegisterInput struct {
	BusinessID uuid.UUID
	Name       string
	Email      string
	Password   string
	Role       string
}

type LoginInput struct {
	Email    string
	Password string
}

type GoogleLoginInput struct {
	Credential    string
	BusinessName  string
	BusinessPhone string
}

type AuthResult struct {
	Token string
	User  *User
}

type Service interface {
	// Register creates an unverified user and sends a verification email.
	Register(input RegisterInput) (*User, error)
	Login(input LoginInput) (*AuthResult, error)
	GoogleLogin(ctx context.Context, input GoogleLoginInput) (*AuthResult, error)
	VerifyEmail(token string) error
	// ResendVerification always succeeds to avoid email enumeration.
	ResendVerification(emailAddress string) error
	LinkGoogle(ctx context.Context, userID uuid.UUID, credential string) (*User, error)
	UnlinkGoogle(userID uuid.UUID) (*User, error)
	CreateTeamUser(input RegisterInput) (*User, error)
	ListTeam(businessID uuid.UUID) ([]User, error)
	GetByID(id uuid.UUID) (*User, error)
}

func validRole(role string) bool { return role == "owner" || role == "manager" || role == "cashier" }

// normalizeEmail trims and lowercases an address so storage and lookups are
// case-insensitive, matching the normalization used by identity providers
// such as Google.
func normalizeEmail(emailAddress string) string {
	return strings.ToLower(strings.TrimSpace(emailAddress))
}

func (s *service) CreateTeamUser(input RegisterInput) (*User, error) {
	if !validRole(input.Role) {
		return nil, ErrInvalidRole
	}
	input.Email = normalizeEmail(input.Email)
	if _, err := s.repo.FindByEmail(input.Email); err == nil {
		return nil, ErrEmailTaken
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	hashStr := string(hash)
	// Team members are provisioned by an authenticated owner, so their address
	// is trusted without a separate verification round-trip.
	user := &User{BusinessID: input.BusinessID, Name: input.Name, Email: input.Email, PasswordHash: &hashStr, Role: input.Role, Active: true, EmailVerified: true}
	if err := s.repo.Create(user); err != nil {
		return nil, err
	}
	return user, nil
}
func (s *service) ListTeam(businessID uuid.UUID) ([]User, error) {
	return s.repo.ListByBusiness(businessID)
}

func (s *service) GetByID(id uuid.UUID) (*User, error) {
	return s.repo.FindByID(id)
}

type service struct {
	repo            Repository
	cfg             *config.Config
	businesses      BusinessChecker
	google          GoogleTokenVerifier
	businessCreator BusinessCreator
	mailer          email.Sender
}

func NewService(repo Repository, cfg *config.Config, businesses BusinessChecker, google GoogleTokenVerifier, businessCreator BusinessCreator, mailer email.Sender) Service {
	return &service{repo: repo, cfg: cfg, businesses: businesses, google: google, businessCreator: businessCreator, mailer: mailer}
}

func (s *service) Register(input RegisterInput) (*User, error) {
	input.Email = normalizeEmail(input.Email)
	exists, err := s.businesses.Exists(input.BusinessID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrBusinessNotFound
	}

	if _, err := s.repo.FindByEmail(input.Email); err == nil {
		return nil, ErrEmailTaken
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	hashStr := string(hash)
	user := &User{
		BusinessID:    input.BusinessID,
		Name:          input.Name,
		Email:         input.Email,
		PasswordHash:  &hashStr,
		Role:          "owner",
		Active:        true,
		EmailVerified: false,
	}

	if err := s.repo.Create(user); err != nil {
		return nil, err
	}

	s.issueEmailVerification(user)
	return user, nil
}

func (s *service) Login(input LoginInput) (*AuthResult, error) {
	input.Email = normalizeEmail(input.Email)
	user, err := s.repo.FindByEmail(input.Email)
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	if user.PasswordHash == nil {
		_ = bcrypt.CompareHashAndPassword(dummyPasswordHash, []byte(input.Password))
		return nil, ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(*user.PasswordHash), []byte(input.Password)); err != nil {
		return nil, ErrInvalidCredentials
	}
	if !user.Active {
		return nil, ErrInvalidCredentials
	}
	if !user.EmailVerified {
		return nil, ErrEmailNotVerified
	}

	return s.result(user)
}

func (s *service) GoogleLogin(ctx context.Context, input GoogleLoginInput) (*AuthResult, error) {
	if s.google == nil || s.cfg.GoogleClientID == "" {
		return nil, ErrGoogleAuthUnavailable
	}

	identity, err := s.google.Verify(ctx, input.Credential)
	if err != nil || identity == nil || identity.Subject == "" || identity.Email == "" {
		return nil, ErrInvalidGoogleToken
	}
	if !identity.EmailVerified {
		return nil, ErrUnverifiedGoogleEmail
	}
	identity.Email = normalizeEmail(identity.Email)

	// 1) A subject already linked to an account signs in directly.
	user, err := s.repo.FindByGoogleID(identity.Subject)
	if err == nil {
		if !user.Active {
			return nil, ErrInactiveUser
		}
		return s.result(user)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	// 2) The email may belong to an existing account.
	user, err = s.repo.FindByEmail(identity.Email)
	if err == nil {
		if !user.Active {
			return nil, ErrInactiveUser
		}
		if user.GoogleID != nil && *user.GoogleID != identity.Subject {
			return nil, ErrGoogleAccountConflict
		}
		if user.GoogleID != nil {
			// Defensive: the subject lookup above should have found this user.
			return nil, ErrGoogleAccountConflict
		}
		if user.EmailVerified {
			// Never auto-link a trusted local account. The owner must prove
			// ownership with their password, then link explicitly.
			if user.PasswordHash != nil {
				return nil, ErrGoogleLinkRequiresPassword
			}
			return nil, ErrGoogleLinkRequired
		}

		// The local account was never verified, so its password owner never
		// proved control of this inbox. Google did. Let the verified Google
		// owner claim the account and drop the untrusted password so the
		// original registrant cannot keep access.
		if err := s.repo.SetGoogleID(user.ID, identity.Subject); err != nil {
			if errors.Is(err, ErrGoogleIDTaken) {
				return nil, ErrGoogleAccountConflict
			}
			return nil, err
		}
		if err := s.repo.MarkEmailVerified(user.ID); err != nil {
			return nil, err
		}
		if user.PasswordHash != nil {
			if err := s.repo.ClearPassword(user.ID); err != nil {
				return nil, err
			}
			user.PasswordHash = nil
		}
		googleID := identity.Subject
		user.GoogleID = &googleID
		user.EmailVerified = true
		return s.result(user)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	// 3) A brand-new Google identity needs a workspace to own.
	businessName := strings.TrimSpace(input.BusinessName)
	if businessName == "" {
		return nil, ErrWorkspaceRequired
	}
	if s.businessCreator == nil {
		return nil, ErrWorkspaceRequired
	}

	name := strings.TrimSpace(identity.Name)
	if name == "" {
		name = identity.Email
	}
	googleID := identity.Subject
	user = &User{
		Name:          name,
		Email:         identity.Email,
		GoogleID:      &googleID,
		Role:          "owner",
		Active:        true,
		EmailVerified: true,
	}
	if err := s.businessCreator.CreateWithOwner(businessName, strings.TrimSpace(input.BusinessPhone), user); err != nil {
		return nil, err
	}

	return s.result(user)
}

func (s *service) issueEmailVerification(user *User) {
	if s.mailer == nil {
		log.Printf("auth: no email sender configured; verification email for %s was not sent", user.Email)
		return
	}
	raw, err := randomToken()
	if err != nil {
		log.Printf("auth: could not generate verification token for %s: %v", user.Email, err)
		return
	}
	ttl := time.Duration(s.cfg.EmailVerificationTTLHours) * time.Hour
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	// Only the newest link should work.
	_ = s.repo.DeleteEmailVerificationTokensForUser(user.ID)
	token := &EmailVerificationToken{
		UserID:    user.ID,
		TokenHash: hashToken(raw),
		ExpiresAt: time.Now().Add(ttl),
	}
	if err := s.repo.CreateEmailVerificationToken(token); err != nil {
		log.Printf("auth: could not persist verification token for %s: %v", user.Email, err)
		return
	}

	link := fmt.Sprintf("%s/verify-email?token=%s", strings.TrimRight(s.cfg.FrontendURL, "/"), url.QueryEscape(raw))
	hours := int(ttl.Hours())
	text := fmt.Sprintf("Hi %s,\n\nConfirm your email address to finish setting up Business OS:\n\n%s\n\nThis link expires in %d hours. If you did not sign up, you can ignore this email.\n", user.Name, link, hours)
	html := fmt.Sprintf("<p>Hi %s,</p><p>Confirm your email address to finish setting up Business OS:</p><p><a href=\"%s\">Verify my email</a></p><p>This link expires in %d hours. If you did not sign up, you can ignore this email.</p>", user.Name, link, hours)
	if err := s.mailer.Send(context.Background(), email.Message{To: user.Email, Subject: "Verify your Business OS email", Text: text, HTML: html}); err != nil {
		log.Printf("auth: could not send verification email to %s: %v", user.Email, err)
	}
}

func (s *service) VerifyEmail(token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return ErrInvalidVerificationToken
	}
	record, err := s.repo.FindEmailVerificationTokenByHash(hashToken(token))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrInvalidVerificationToken
		}
		return err
	}
	if record.UsedAt != nil {
		return ErrInvalidVerificationToken
	}
	if time.Now().After(record.ExpiresAt) {
		return ErrExpiredVerificationToken
	}
	consumed, err := s.repo.ConsumeEmailVerificationToken(record.ID)
	if err != nil {
		return err
	}
	if !consumed {
		return ErrInvalidVerificationToken
	}
	if err := s.repo.MarkEmailVerified(record.UserID); err != nil {
		return err
	}
	_ = s.repo.DeleteEmailVerificationTokensForUser(record.UserID)
	return nil
}

func (s *service) ResendVerification(emailAddress string) error {
	emailAddress = normalizeEmail(emailAddress)
	user, err := s.repo.FindByEmail(emailAddress)
	if err != nil || !user.Active || user.EmailVerified {
		// Deliberately generic: revealing whether an address exists, is active,
		// or is already verified enables enumeration.
		return nil
	}
	s.issueEmailVerification(user)
	return nil
}

func (s *service) LinkGoogle(ctx context.Context, userID uuid.UUID, credential string) (*User, error) {
	if s.google == nil || s.cfg.GoogleClientID == "" {
		return nil, ErrGoogleAuthUnavailable
	}
	identity, err := s.google.Verify(ctx, credential)
	if err != nil || identity == nil || identity.Subject == "" || identity.Email == "" {
		return nil, ErrInvalidGoogleToken
	}
	if !identity.EmailVerified {
		return nil, ErrUnverifiedGoogleEmail
	}

	user, err := s.repo.FindByID(userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	if !user.Active {
		return nil, ErrInactiveUser
	}
	if normalizeEmail(identity.Email) != normalizeEmail(user.Email) {
		return nil, ErrGoogleEmailMismatch
	}
	if user.GoogleID != nil {
		if *user.GoogleID == identity.Subject {
			return user, nil
		}
		return nil, ErrGoogleAccountConflict
	}

	if existing, err := s.repo.FindByGoogleID(identity.Subject); err == nil && existing.ID != user.ID {
		return nil, ErrGoogleIDTaken
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	if err := s.repo.SetGoogleID(user.ID, identity.Subject); err != nil {
		return nil, err
	}
	if !user.EmailVerified {
		if err := s.repo.MarkEmailVerified(user.ID); err != nil {
			return nil, err
		}
	}
	googleID := identity.Subject
	user.GoogleID = &googleID
	user.EmailVerified = true
	return user, nil
}

func (s *service) UnlinkGoogle(userID uuid.UUID) (*User, error) {
	user, err := s.repo.FindByID(userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	if user.PasswordHash == nil {
		return nil, ErrCannotUnlinkGoogleOnly
	}
	if err := s.repo.ClearGoogleID(user.ID); err != nil {
		return nil, err
	}
	user.GoogleID = nil
	return user, nil
}

func (s *service) result(user *User) (*AuthResult, error) {
	token, err := s.generateToken(user)
	if err != nil {
		return nil, err
	}
	return &AuthResult{Token: token, User: user}, nil
}

func (s *service) generateToken(user *User) (string, error) {
	claims := jwt.MapClaims{
		"user_id":     user.ID.String(),
		"business_id": user.BusinessID.String(),
		"role":        user.Role,
		"exp":         time.Now().Add(time.Duration(s.cfg.JWTExpiryHours) * time.Hour).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.cfg.JWTSecret))
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
