package payments

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/businessos/backend/internal/config"
)

var (
	ErrInvalidSignature  = errors.New("invalid callback signature")
	ErrMpesaClientFailed = errors.New("failed to connect to m-pesa gateway")
	ErrMpesaAuthFailed   = errors.New("m-pesa authentication failed")
	ErrMissingConfig     = errors.New("m-pesa configuration is incomplete")
	ErrTransactionFailed = errors.New("m-pesa transaction request failed")
	// ErrTransactionAmbiguous means the gateway outcome is indeterminate (for
	// example a 5xx after the request may have been accepted). The transaction
	// must stay in a non-final state so a late callback can still settle it and
	// a retry cannot trigger a second payout.
	ErrTransactionAmbiguous = errors.New("m-pesa transaction outcome is ambiguous")
	// ErrMpesaPreDispatch means the request failed before it was sent to the
	// payment endpoint (for example OAuth/token acquisition). The reservation can
	// safely be released so a retry may dispatch.
	ErrMpesaPreDispatch = errors.New("m-pesa request failed before dispatch")
)

type MpesaClient interface {
	InitiateSTKPush(ctx context.Context, req DarajaSTKPushRequest) (*DarajaSTKPushResponse, error)
	InitiateB2CPayout(ctx context.Context, req DarajaB2CRequest) (*DarajaB2CResponse, error)
}

type mpesaClient struct {
	cfg        *config.Config
	httpClient *http.Client
	baseURL    string
}

func NewMpesaClient(cfg *config.Config) MpesaClient {
	baseURL := "https://sandbox.safaricom.co.ke"
	if strings.EqualFold(cfg.MpesaEnv, "production") {
		baseURL = "https://api.safaricom.co.ke"
	}
	return &mpesaClient{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
		baseURL: baseURL,
	}
}

// GetOAuthToken retrieves an OAuth access token using consumer key & secret.
func (c *mpesaClient) getOAuthToken(ctx context.Context) (string, error) {
	if c.cfg.MpesaConsumerKey == "" || c.cfg.MpesaConsumerSecret == "" {
		return "", ErrMissingConfig
	}

	url := fmt.Sprintf("%s/oauth/v1/generate?grant_type=client_credentials", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build token request: %w", err)
	}

	auth := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", c.cfg.MpesaConsumerKey, c.cfg.MpesaConsumerSecret)))
	req.Header.Set("Authorization", "Basic "+auth)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrMpesaClientFailed, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("%w: status %d body %s", ErrMpesaAuthFailed, resp.StatusCode, string(bodyBytes))
	}

	var authResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   string `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}

	return authResp.AccessToken, nil
}

// InitiateSTKPush sends Lipa Na M-Pesa Online STK push prompt.
func (c *mpesaClient) InitiateSTKPush(ctx context.Context, req DarajaSTKPushRequest) (*DarajaSTKPushResponse, error) {
	token, err := c.getOAuthToken(ctx)
	if err != nil {
		// Token acquisition happens before the payment POST is sent.
		return nil, fmt.Errorf("%w: %v", ErrMpesaPreDispatch, err)
	}

	//nolint:gosec // G117: Daraja STK push protocol requires password field in JSON body
	payloadBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal stk request: %w", err)
	}

	url := fmt.Sprintf("%s/mpesa/stkpush/v1/processrequest", c.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("build stk request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMpesaClientFailed, err)
	}
	defer func() { _ = resp.Body.Close() }()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	var stkResp DarajaSTKPushResponse
	if err := json.Unmarshal(bodyBytes, &stkResp); err != nil {
		return nil, fmt.Errorf("decode stk response: %w", err)
	}

	// A non-zero ResponseCode is an explicit rejection regardless of HTTP status.
	if stkResp.ResponseCode != "" && stkResp.ResponseCode != "0" {
		return &stkResp, fmt.Errorf("%w: code %s, desc %s", ErrTransactionFailed, stkResp.ResponseCode, stkResp.ResponseDescription)
	}
	// A 5xx (or any accepted-looking body on a server error) does not prove the
	// request was rejected: Daraja may still process it. Keep it non-final.
	if resp.StatusCode >= 500 {
		return &stkResp, fmt.Errorf("%w: status %d, desc %s", ErrTransactionAmbiguous, resp.StatusCode, stkResp.ResponseDescription)
	}
	// A 4xx is a definitive rejection.
	if resp.StatusCode >= 400 {
		return &stkResp, fmt.Errorf("%w: status %d, desc %s", ErrTransactionFailed, resp.StatusCode, stkResp.ResponseDescription)
	}
	if stkResp.CheckoutRequestID == "" {
		return &stkResp, fmt.Errorf("%w: response lacks CheckoutRequestID", ErrTransactionFailed)
	}

	return &stkResp, nil
}

// InitiateB2CPayout sends B2C payment request.
func (c *mpesaClient) InitiateB2CPayout(ctx context.Context, req DarajaB2CRequest) (*DarajaB2CResponse, error) {
	token, err := c.getOAuthToken(ctx)
	if err != nil {
		// Token acquisition happens before the payment POST is sent.
		return nil, fmt.Errorf("%w: %v", ErrMpesaPreDispatch, err)
	}

	payloadBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal b2c request: %w", err)
	}

	url := fmt.Sprintf("%s/mpesa/b2c/v1/paymentrequest", c.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("build b2c request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMpesaClientFailed, err)
	}
	defer func() { _ = resp.Body.Close() }()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	var b2cResp DarajaB2CResponse
	if err := json.Unmarshal(bodyBytes, &b2cResp); err != nil {
		return nil, fmt.Errorf("decode b2c response: %w", err)
	}

	// A non-zero ResponseCode is an explicit rejection regardless of HTTP status.
	if b2cResp.ResponseCode != "" && b2cResp.ResponseCode != "0" {
		return &b2cResp, fmt.Errorf("%w: code %s, desc %s", ErrTransactionFailed, b2cResp.ResponseCode, b2cResp.ResponseDescription)
	}
	// A 5xx does not prove the payout was rejected: Daraja may have queued it.
	if resp.StatusCode >= 500 {
		return &b2cResp, fmt.Errorf("%w: status %d, desc %s", ErrTransactionAmbiguous, resp.StatusCode, b2cResp.ResponseDescription)
	}
	if resp.StatusCode >= 400 {
		return &b2cResp, fmt.Errorf("%w: status %d, desc %s", ErrTransactionFailed, resp.StatusCode, b2cResp.ResponseDescription)
	}
	if b2cResp.ConversationID == "" {
		return &b2cResp, fmt.Errorf("%w: response lacks ConversationID", ErrTransactionFailed)
	}

	return &b2cResp, nil
}

// GeneratePassword generates the Daraja STK push password: base64(ShortCode + Passkey + Timestamp).
func GeneratePassword(shortCode, passkey, timestamp string) string {
	raw := fmt.Sprintf("%s%s%s", shortCode, passkey, timestamp)
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

// NormalizePhone converts variations like "+254712345678", "0712345678", "712345678" into "254712345678".
func NormalizePhone(phone string) string {
	cleaned := strings.TrimSpace(phone)
	cleaned = strings.TrimPrefix(cleaned, "+")
	if strings.HasPrefix(cleaned, "0") && len(cleaned) == 10 {
		return "254" + cleaned[1:]
	}
	if (strings.HasPrefix(cleaned, "7") || strings.HasPrefix(cleaned, "1")) && len(cleaned) == 9 {
		return "254" + cleaned
	}
	return cleaned
}

// ValidateHMACSignature checks only the HMAC-SHA256 representation of the body
// (hex or base64). It deliberately does not accept the bare secret, so a URL
// token can never double as a signing key.
func ValidateHMACSignature(secret string, body []byte, signatureHeader string) bool {
	secret = strings.TrimSpace(secret)
	signatureHeader = strings.TrimSpace(signatureHeader)
	if secret == "" || signatureHeader == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expectedMAC := mac.Sum(nil)
	expectedHex := hex.EncodeToString(expectedMAC)
	expectedB64 := base64.StdEncoding.EncodeToString(expectedMAC)
	if hmac.Equal([]byte(strings.ToLower(signatureHeader)), []byte(strings.ToLower(expectedHex))) {
		return true
	}
	return hmac.Equal([]byte(signatureHeader), []byte(expectedB64))
}

// DeriveCallbackToken returns a stable token for provider callback URLs without
// exposing the HMAC signing secret. It is HMAC(secret, label), so it cannot be
// reversed into the secret.
func DeriveCallbackToken(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("mpesa-callback-url"))
	return hex.EncodeToString(mac.Sum(nil))
}

// ValidateCallbackSignature validates callback signature against MPESA_CALLBACK_SECRET.
// Supports direct shared secret token header or HMAC-SHA256 (hex or base64).
func ValidateCallbackSignature(secret string, body []byte, signatureHeader string) bool {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		// Secret is required; empty secret must reject all callbacks
		return false
	}
	signatureHeader = strings.TrimSpace(signatureHeader)
	if signatureHeader == "" {
		return false
	}
	// Direct secret token comparison (constant-time)
	if hmac.Equal([]byte(signatureHeader), []byte(secret)) {
		return true
	}

	// HMAC-SHA256 comparison
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expectedMAC := mac.Sum(nil)
	expectedHex := hex.EncodeToString(expectedMAC)
	expectedB64 := base64.StdEncoding.EncodeToString(expectedMAC)

	if hmac.Equal([]byte(strings.ToLower(signatureHeader)), []byte(strings.ToLower(expectedHex))) {
		return true
	}
	if hmac.Equal([]byte(signatureHeader), []byte(expectedB64)) {
		return true
	}

	return false
}
