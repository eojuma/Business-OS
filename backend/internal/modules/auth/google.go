package auth

import (
	"context"
	"fmt"

	"google.golang.org/api/idtoken"
)

type tokenValidator func(ctx context.Context, credential, audience string) (*idtoken.Payload, error)

type idTokenVerifier struct {
	audience string
	validate tokenValidator
}

// NewGoogleTokenVerifier verifies Google Identity Services ID tokens. It
// returns a nil verifier (and no error) when clientID is empty so the API can
// still start without Google sign-in configured.
func NewGoogleTokenVerifier(ctx context.Context, clientID string) (GoogleTokenVerifier, error) {
	if clientID == "" {
		return nil, nil
	}
	validator, err := idtoken.NewValidator(ctx)
	if err != nil {
		return nil, fmt.Errorf("create google id token validator: %w", err)
	}
	return &idTokenVerifier{audience: clientID, validate: validator.Validate}, nil
}

func (v *idTokenVerifier) Verify(ctx context.Context, credential string) (*GoogleIdentity, error) {
	payload, err := v.validate(ctx, credential, v.audience)
	if err != nil {
		return nil, err
	}
	if payload.Issuer != "accounts.google.com" && payload.Issuer != "https://accounts.google.com" {
		return nil, fmt.Errorf("google id token: unexpected issuer %q", payload.Issuer)
	}

	email, _ := payload.Claims["email"].(string)
	verified, _ := payload.Claims["email_verified"].(bool)
	name, _ := payload.Claims["name"].(string)

	return &GoogleIdentity{
		Subject:       payload.Subject,
		Email:         email,
		EmailVerified: verified,
		Name:          name,
	}, nil
}
