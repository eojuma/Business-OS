package receipts

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/businessos/backend/internal/config"
)

// OCRClient turns a receipt image into structured line items. It is the only
// thing this module knows about talking to the OCR provider, mirroring how the
// assistant funnels everything through its own AI client.
type OCRClient interface {
	Extract(ctx context.Context, image []byte, contentType string) (*Extraction, error)
}

type visionClient struct {
	apiKey  string
	baseURL string
	model   string
	http    *http.Client
}

// NewOCRClient builds the vision-based OCR client. It reuses the assistant's
// OpenAI-compatible provider (see docs/OCR.md) and prefers AI_OCR_MODEL when
// set, falling back to AI_MODEL.
func NewOCRClient(cfg *config.Config) OCRClient {
	model := strings.TrimSpace(cfg.AIOCRModel)
	if model == "" {
		model = cfg.AIModel
	}
	return &visionClient{
		apiKey:  strings.TrimSpace(cfg.AIAPIKey),
		baseURL: strings.TrimRight(strings.TrimSpace(cfg.AIBaseURL), "/"),
		model:   model,
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL string `json:"url"`
}

type visionMessage struct {
	Role    string        `json:"role"`
	Content []contentPart `json:"content"`
}

type visionRequest struct {
	Model    string          `json:"model"`
	Messages []visionMessage `json:"messages"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// Extract sends the image to the vision model and parses its JSON reply.
func (c *visionClient) Extract(ctx context.Context, image []byte, contentType string) (*Extraction, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("AI_API_KEY is not configured")
	}
	if c.baseURL == "" {
		return nil, fmt.Errorf("AI_BASE_URL is not configured")
	}

	dataURL := "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(image)
	body, err := json.Marshal(visionRequest{
		Model: c.model,
		Messages: []visionMessage{
			{
				Role: "user",
				Content: []contentPart{
					{Type: "text", Text: ocrPrompt},
					{Type: "image_url", ImageURL: &imageURL{URL: dataURL}},
				},
			},
		},
	})
	if err != nil {
		return nil, err
	}

	// Transient provider hiccups (rate limits, 5xx) are common on free tiers,
	// so try twice before giving up, matching the assistant client.
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(600 * time.Millisecond):
			}
		}
		content, retryable, err := c.do(ctx, body)
		if err == nil {
			return parseExtractionJSON(content)
		}
		lastErr = err
		if !retryable {
			break
		}
	}
	return nil, lastErr
}

func (c *visionClient) do(ctx context.Context, body []byte) (content string, retryable bool, err error) {
	endpoint := c.baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", true, fmt.Errorf("ocr request to %s failed: %w", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", true, err
	}

	if resp.StatusCode != http.StatusOK {
		retryable = resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		return "", retryable, fmt.Errorf("ocr request to %s with model %q failed (%d): %s", endpoint, c.model, resp.StatusCode, string(respBody))
	}

	var parsed chatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", false, err
	}
	if len(parsed.Choices) == 0 {
		return "", false, fmt.Errorf("ocr returned no choices")
	}
	return parsed.Choices[0].Message.Content, false, nil
}

const ocrPrompt = `You are an OCR engine for a hardware store in Kenya. Read the photographed receipt and return ONLY a JSON object, no markdown fences and no explanation, in exactly this shape:
{"merchant": "<store name or empty>", "date": "<date as printed or empty>", "currency": "<e.g. KSh>", "line_items": [{"description": "<item name>", "quantity": <number>, "unit_price": <number>, "subtotal": <number>}], "total": <number>, "confidence": "high|medium|low", "notes": "<anything unclear or handwritten, or empty>"}

Rules:
- Amounts are decimal numbers in the receipt's currency (for example 450.50), never cents.
- quantity defaults to 1 when not printed.
- When a value is unreadable, use 0 and mention it in notes.
- Do not invent items. Return fewer items rather than guessing.
- The receipt may be handwritten or informal; use "low" confidence when unsure.`

// Extraction is the raw shape the OCR model returns.
type Extraction struct {
	Merchant   string              `json:"merchant"`
	Date       string              `json:"date"`
	Currency   string              `json:"currency"`
	LineItems  []ExtractedLineItem `json:"line_items"`
	Total      flexMoney           `json:"total"`
	Confidence string              `json:"confidence"`
	Notes      string              `json:"notes"`
}

// ExtractedLineItem is one line as read off the receipt.
type ExtractedLineItem struct {
	Description string    `json:"description"`
	Quantity    flexInt   `json:"quantity"`
	UnitPrice   flexMoney `json:"unit_price"`
	Subtotal    flexMoney `json:"subtotal"`
}

// flexInt accepts a JSON number or a numeric string, since models sometimes
// quote quantities.
type flexInt int64

func (f *flexInt) UnmarshalJSON(data []byte) error {
	raw := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if raw == "" || raw == "null" {
		*f = 0
		return nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return err
	}
	*f = flexInt(int64(value))
	return nil
}

// flexMoney accepts a JSON number or string and converts it to int64 cents.
type flexMoney int64

func (m *flexMoney) UnmarshalJSON(data []byte) error {
	raw := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if raw == "" || raw == "null" {
		*m = 0
		return nil
	}
	cents, err := parseMoneyToCents(raw)
	if err != nil {
		return err
	}
	*m = flexMoney(cents)
	return nil
}

// parseMoneyToCents converts a printed money value such as "1,250.50",
// "KSh 450" or "450.5" into int64 cents without going through float64, to
// avoid rounding drift.
func parseMoneyToCents(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}

	negative := strings.HasPrefix(raw, "-")
	var b strings.Builder
	for _, r := range raw {
		if (r >= '0' && r <= '9') || r == '.' {
			b.WriteRune(r)
		}
	}
	cleaned := b.String()
	if cleaned == "" || cleaned == "." {
		return 0, fmt.Errorf("invalid money value %q", raw)
	}

	parts := strings.SplitN(cleaned, ".", 2)
	var whole int64
	if parts[0] != "" {
		parsed, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid money value %q: %w", raw, err)
		}
		whole = parsed
	}

	var frac int64
	if len(parts) == 2 {
		digits := parts[1]
		if len(digits) > 2 {
			digits = digits[:2]
		}
		for len(digits) < 2 {
			digits += "0"
		}
		parsed, err := strconv.ParseInt(digits, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid money value %q: %w", raw, err)
		}
		frac = parsed
	}

	cents := whole*100 + frac
	if negative {
		cents = -cents
	}
	return cents, nil
}

func parseExtractionJSON(raw string) (*Extraction, error) {
	cleaned := strings.TrimSpace(raw)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	// Models sometimes wrap the object in prose; keep only the outer braces.
	if start := strings.Index(cleaned, "{"); start >= 0 {
		if end := strings.LastIndex(cleaned, "}"); end > start {
			cleaned = cleaned[start : end+1]
		}
	}

	var parsed Extraction
	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		return nil, err
	}
	return &parsed, nil
}
