package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/businessos/backend/internal/config"
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
	ErrGoogleLinkRequiresPassword = errors.New("this email already has a password; sign in with your password instead")
	ErrWorkspaceRequired          = errors.New("no workspace is linked to this google account; create one to continue")
	ErrInactiveUser               = errors.New("user account is inactive")
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
	Register(input RegisterInput) (*AuthResult, error)
	Login(input LoginInput) (*AuthResult, error)
	GoogleLogin(ctx context.Context, input GoogleLoginInput) (*AuthResult, error)
	CreateTeamUser(input RegisterInput) (*User, error)
	ListTeam(businessID uuid.UUID) ([]User, error)
	GetByID(id uuid.UUID) (*User, error)
}

func validRole(role string) bool { return role == "owner" || role == "manager" || role == "cashier" }

// normalizeEmail trims and lowercases an address so storage and lookups are
// case-insensitive, matching the normalization used by identity providers
// such as Google.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
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
	user := &User{BusinessID: input.BusinessID, Name: input.Name, Email: input.Email, PasswordHash: &hashStr, Role: input.Role, Active: true}
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
}

func NewService(repo Repository, cfg *config.Config, businesses BusinessChecker, google GoogleTokenVerifier, businessCreator BusinessCreator) Service {
	return &service{repo: repo, cfg: cfg, businesses: businesses, google: google, businessCreator: businessCreator}
}

func (s *service) Register(input RegisterInput) (*AuthResult, error) {
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
		BusinessID:   input.BusinessID,
		Name:         input.Name,
		Email:        input.Email,
		PasswordHash: &hashStr,
		Role:         "owner",
		Active:       true,
	}

	if err := s.repo.Create(user); err != nil {
		return nil, err
	}

	return s.result(user)
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

	user, err = s.repo.FindByEmail(identity.Email)
	if err == nil {
		if !user.Active {
			return nil, ErrInactiveUser
		}
		if user.GoogleID != nil && *user.GoogleID != identity.Subject {
			return nil, ErrGoogleAccountConflict
		}
		if user.GoogleID == nil {
			if user.PasswordHash != nil {
				return nil, ErrGoogleLinkRequiresPassword
			}
			if err := s.repo.SetGoogleID(user.ID, identity.Subject); err != nil {
				return nil, err
			}
			user.GoogleID = &identity.Subject
		}
		return s.result(user)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	// A Google identity without an existing user needs a workspace to join.
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
		Name:     name,
		Email:    identity.Email,
		GoogleID: &googleID,
		Role:     "owner",
		Active:   true,
	}
	if err := s.businessCreator.CreateWithOwner(businessName, strings.TrimSpace(input.BusinessPhone), user); err != nil {
		return nil, err
	}

	return s.result(user)
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
