package auth

import (
	"errors"
	"net/http"

	"github.com/businessos/backend/internal/shared/middleware"
	"github.com/businessos/backend/internal/shared/response"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Handler struct {
	service Service
}

type teamUserRequest struct {
	Name     string `json:"name" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8"`
	Role     string `json:"role" binding:"required"`
}

func (h *Handler) CreateTeamUser(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}
	var req teamUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	user, err := h.service.CreateTeamUser(RegisterInput{BusinessID: businessID, Name: req.Name, Email: req.Email, Password: req.Password, Role: req.Role})
	if errors.Is(err, ErrEmailTaken) {
		response.Error(c, http.StatusConflict, err.Error())
		return
	}
	if errors.Is(err, ErrInvalidRole) {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to create user")
		return
	}
	response.Success(c, http.StatusCreated, user)
}
func (h *Handler) ListTeam(c *gin.Context) {
	businessID, err := middleware.CurrentBusinessID(c)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}
	users, err := h.service.ListTeam(businessID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to list users")
		return
	}
	response.Success(c, http.StatusOK, users)
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Me(c *gin.Context) {
	userIDStr, exists := c.Get(middleware.ContextUserIDKey)
	if !exists {
		response.Error(c, http.StatusUnauthorized, "user not found in context")
		return
	}
	userID, err := uuid.Parse(userIDStr.(string)) //nolint:errcheck
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid user id")
		return
	}
	user, err := h.service.GetByID(userID)
	if err != nil {
		response.Error(c, http.StatusNotFound, "user not found")
		return
	}
	user.GoogleLinked = user.GoogleID != nil
	response.Success(c, http.StatusOK, user)
}

type registerRequest struct {
	BusinessID string `json:"business_id" binding:"required,uuid"`
	Name       string `json:"name" binding:"required"`
	Email      string `json:"email" binding:"required,email"`
	Password   string `json:"password" binding:"required,min=8"`
	Role       string `json:"role"`
}

func (h *Handler) Register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	businessID, err := uuid.Parse(req.BusinessID)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid business_id")
		return
	}

	user, err := h.service.Register(RegisterInput{
		BusinessID: businessID,
		Name:       req.Name,
		Email:      req.Email,
		Password:   req.Password,
		Role:       req.Role,
	})
	if err != nil {
		if errors.Is(err, ErrBusinessNotFound) {
			response.Error(c, http.StatusBadRequest, err.Error())
			return
		}
		if errors.Is(err, ErrEmailTaken) {
			response.Error(c, http.StatusConflict, err.Error())
			return
		}
		response.Error(c, http.StatusInternalServerError, "failed to register user")
		return
	}
	// No token is issued until the owner confirms their email address.
	response.Success(c, http.StatusCreated, gin.H{
		"user":                        user,
		"email_verification_required": true,
	})
}

type loginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

func (h *Handler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	result, err := h.service.Login(LoginInput{
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			response.Error(c, http.StatusUnauthorized, err.Error())
			return
		}
		if errors.Is(err, ErrEmailNotVerified) {
			response.ErrorWithCode(c, http.StatusForbidden, "email_not_verified", err.Error())
			return
		}
		response.Error(c, http.StatusInternalServerError, "failed to login")
		return
	}

	response.Success(c, http.StatusOK, gin.H{
		"token": result.Token,
		"user":  result.User,
	})
}

type googleRequest struct {
	Credential    string `json:"credential" binding:"required"`
	BusinessName  string `json:"business_name"`
	BusinessPhone string `json:"business_phone"`
}

func (h *Handler) Google(c *gin.Context) {
	var req googleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	result, err := h.service.GoogleLogin(c.Request.Context(), GoogleLoginInput{
		Credential:    req.Credential,
		BusinessName:  req.BusinessName,
		BusinessPhone: req.BusinessPhone,
	})
	switch {
	case err == nil:
	case errors.Is(err, ErrGoogleAuthUnavailable):
		response.Error(c, http.StatusServiceUnavailable, err.Error())
		return
	case errors.Is(err, ErrInvalidGoogleToken), errors.Is(err, ErrUnverifiedGoogleEmail):
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	case errors.Is(err, ErrWorkspaceRequired):
		response.ErrorWithCode(c, http.StatusConflict, "workspace_required", err.Error())
		return
	case errors.Is(err, ErrGoogleAccountConflict):
		response.ErrorWithCode(c, http.StatusConflict, "google_account_conflict", err.Error())
		return
	case errors.Is(err, ErrGoogleLinkRequiresPassword):
		response.ErrorWithCode(c, http.StatusConflict, "google_link_requires_password", err.Error())
		return
	case errors.Is(err, ErrGoogleLinkRequired):
		response.ErrorWithCode(c, http.StatusConflict, "google_link_required", err.Error())
		return
	case errors.Is(err, ErrEmailTaken):
		response.ErrorWithCode(c, http.StatusConflict, "email_taken", err.Error())
		return
	case errors.Is(err, ErrInactiveUser):
		response.Error(c, http.StatusForbidden, err.Error())
		return
	default:
		response.Error(c, http.StatusInternalServerError, "failed to sign in with google")
		return
	}

	response.Success(c, http.StatusOK, gin.H{
		"token": result.Token,
		"user":  result.User,
	})
}

func currentUserID(c *gin.Context) (uuid.UUID, bool) {
	value, exists := c.Get(middleware.ContextUserIDKey)
	if !exists {
		response.Error(c, http.StatusUnauthorized, "user not found in context")
		return uuid.Nil, false
	}
	raw, ok := value.(string)
	if !ok {
		response.Error(c, http.StatusBadRequest, "invalid user id")
		return uuid.Nil, false
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid user id")
		return uuid.Nil, false
	}
	return id, true
}

type verifyEmailRequest struct {
	Token string `json:"token" binding:"required"`
}

// VerifyEmail consumes a single-use verification token and marks the account's
// email as confirmed.
func (h *Handler) VerifyEmail(c *gin.Context) {
	var req verifyEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	err := h.service.VerifyEmail(req.Token)
	switch {
	case err == nil:
		response.Success(c, http.StatusOK, gin.H{"verified": true})
	case errors.Is(err, ErrExpiredVerificationToken):
		response.ErrorWithCode(c, http.StatusGone, "verification_expired", err.Error())
	case errors.Is(err, ErrInvalidVerificationToken):
		response.ErrorWithCode(c, http.StatusBadRequest, "verification_invalid", err.Error())
	default:
		response.Error(c, http.StatusInternalServerError, "failed to verify email")
	}
}

type resendVerificationRequest struct {
	Email string `json:"email" binding:"required,email"`
}

// ResendVerification always returns 202 so callers cannot probe which
// addresses exist or are already verified.
func (h *Handler) ResendVerification(c *gin.Context) {
	var req resendVerificationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	_ = h.service.ResendVerification(req.Email)
	response.Success(c, http.StatusAccepted, gin.H{"sent": true})
}

type googleLinkRequest struct {
	Credential string `json:"credential" binding:"required"`
}

// LinkGoogle attaches a Google identity to the signed-in account after the
// user proves ownership with their existing session.
func (h *Handler) LinkGoogle(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var req googleLinkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	user, err := h.service.LinkGoogle(c.Request.Context(), userID, req.Credential)
	switch {
	case err == nil:
		user.GoogleLinked = user.GoogleID != nil
		response.Success(c, http.StatusOK, user)
	case errors.Is(err, ErrGoogleAuthUnavailable):
		response.Error(c, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, ErrInvalidGoogleToken),
		errors.Is(err, ErrUnverifiedGoogleEmail),
		errors.Is(err, ErrInvalidCredentials):
		response.Error(c, http.StatusUnauthorized, err.Error())
	case errors.Is(err, ErrInactiveUser):
		response.Error(c, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrGoogleEmailMismatch):
		response.ErrorWithCode(c, http.StatusBadRequest, "google_email_mismatch", err.Error())
	case errors.Is(err, ErrGoogleAccountConflict), errors.Is(err, ErrGoogleIDTaken):
		response.ErrorWithCode(c, http.StatusConflict, "google_account_conflict", err.Error())
	default:
		response.Error(c, http.StatusInternalServerError, "failed to connect google account")
	}
}

// UnlinkGoogle detaches the Google identity, refusing when it is the only way
// the account can sign in.
func (h *Handler) UnlinkGoogle(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	user, err := h.service.UnlinkGoogle(userID)
	switch {
	case err == nil:
		user.GoogleLinked = false
		response.Success(c, http.StatusOK, user)
	case errors.Is(err, ErrCannotUnlinkGoogleOnly):
		response.ErrorWithCode(c, http.StatusConflict, "cannot_unlink_google_only", err.Error())
	case errors.Is(err, ErrInvalidCredentials):
		response.Error(c, http.StatusNotFound, "user not found")
	default:
		response.Error(c, http.StatusInternalServerError, "failed to disconnect google account")
	}
}
