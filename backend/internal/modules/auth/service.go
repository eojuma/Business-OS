package auth

import (
	"errors"
	"time"

	"github.com/businessos/backend/internal/config"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrEmailTaken         = errors.New("email already registered")
	ErrBusinessNotFound   = errors.New("business does not exist")
	ErrInvalidRole        = errors.New("role must be owner, manager, or cashier")
)

type BusinessChecker interface {
	Exists(id uuid.UUID) (bool, error)
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

type AuthResult struct {
	Token string
	User  *User
}

type Service interface {
	Register(input RegisterInput) (*AuthResult, error)
	Login(input LoginInput) (*AuthResult, error)
	CreateTeamUser(input RegisterInput) (*User, error)
	ListTeam(businessID uuid.UUID) ([]User, error)
}

func validRole(role string) bool { return role == "owner" || role == "manager" || role == "cashier" }

func (s *service) CreateTeamUser(input RegisterInput) (*User, error) {
	if !validRole(input.Role) {
		return nil, ErrInvalidRole
	}
	if _, err := s.repo.FindByEmail(input.Email); err == nil {
		return nil, ErrEmailTaken
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	user := &User{BusinessID: input.BusinessID, Name: input.Name, Email: input.Email, PasswordHash: string(hash), Role: input.Role, Active: true}
	if err := s.repo.Create(user); err != nil {
		return nil, err
	}
	return user, nil
}
func (s *service) ListTeam(businessID uuid.UUID) ([]User, error) {
	return s.repo.ListByBusiness(businessID)
}

type service struct {
	repo       Repository
	cfg        *config.Config
	businesses BusinessChecker
}

func NewService(repo Repository, cfg *config.Config, businesses BusinessChecker) Service {
	return &service{repo: repo, cfg: cfg, businesses: businesses}
}

func (s *service) Register(input RegisterInput) (*AuthResult, error) {
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

	user := &User{
		BusinessID:   input.BusinessID,
		Name:         input.Name,
		Email:        input.Email,
		PasswordHash: string(hash),
		Role:         "owner",
		Active:       true,
	}

	if err := s.repo.Create(user); err != nil {
		return nil, err
	}

	token, err := s.generateToken(user)
	if err != nil {
		return nil, err
	}

	return &AuthResult{Token: token, User: user}, nil
}

func (s *service) Login(input LoginInput) (*AuthResult, error) {
	user, err := s.repo.FindByEmail(input.Email)
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(input.Password)); err != nil {
		return nil, ErrInvalidCredentials
	}
	if !user.Active {
		return nil, ErrInvalidCredentials
	}

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
