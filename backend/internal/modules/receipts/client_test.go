package receipts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/businessos/backend/internal/config"
)

func TestExtractSendsVisionRequest(t *testing.T) {
	var gotPath, gotAuth, gotModel, gotImageURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")

		var body struct {
			Model    string `json:"model"`
			Messages []struct {
				Content []struct {
					Type     string `json:"type"`
					ImageURL struct {
						URL string `json:"url"`
					} `json:"image_url"`
				} `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		gotModel = body.Model
		for _, part := range body.Messages[0].Content {
			if part.Type == "image_url" {
				gotImageURL = part.ImageURL.URL
			}
		}

		reply := `{"merchant":"Shop","currency":"KSh","line_items":[{"description":"Cement","quantity":2,"unit_price":450.50,"subtotal":901}],"total":901,"confidence":"high"}`
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": reply}}},
		})
	}))
	defer srv.Close()

	client := NewOCRClient(&config.Config{
		AIAPIKey:   "key",
		AIBaseURL:  srv.URL + "/",
		AIModel:    "text-model",
		AIOCRModel: "vision-model",
	})

	extraction, err := client.Extract(context.Background(), []byte("image-bytes"), contentTypeJPEG)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotPath != "/chat/completions" {
		t.Fatalf("got path %q, want /chat/completions", gotPath)
	}
	if gotAuth != "Bearer key" {
		t.Fatalf("got auth %q, want the bearer key", gotAuth)
	}
	if gotModel != "vision-model" {
		t.Fatalf("got model %q, want AI_OCR_MODEL to be preferred", gotModel)
	}
	if !strings.HasPrefix(gotImageURL, "data:image/jpeg;base64,") {
		t.Fatalf("got image url %q, want a base64 jpeg data URL", gotImageURL)
	}
	if len(extraction.LineItems) != 1 || int64(extraction.LineItems[0].UnitPrice) != 45050 {
		t.Fatalf("got extraction %+v, want 1 line priced at 45050 cents", extraction.LineItems)
	}
	if int64(extraction.Total) != 90100 {
		t.Fatalf("got total %d, want 90100 cents", extraction.Total)
	}
}

func TestOCRClientFallsBackToAIModel(t *testing.T) {
	client := NewOCRClient(&config.Config{AIAPIKey: "key", AIBaseURL: "https://example.com/v1", AIModel: "gemini-flash"})
	vision, ok := client.(*visionClient)
	if !ok {
		t.Fatalf("got client type %T, want *visionClient", client)
	}
	if vision.model != "gemini-flash" {
		t.Fatalf("got model %q, want the AI_MODEL fallback", vision.model)
	}
}

func TestExtractRejectsMissingAPIKey(t *testing.T) {
	client := NewOCRClient(&config.Config{AIAPIKey: "  ", AIBaseURL: "https://example.com/v1", AIModel: "m"})
	if _, err := client.Extract(context.Background(), []byte("x"), contentTypeJPEG); err == nil || !strings.Contains(err.Error(), "AI_API_KEY") {
		t.Fatalf("got err %v, want an AI_API_KEY configuration error", err)
	}
}

func TestParseMoneyToCents(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"450", 45000},
		{"450.5", 45050},
		{"450.50", 45050},
		{"1,250.75", 125075},
		{"KSh 1,000.00", 100000},
		{".50", 50},
		{"-200", -20000},
		{"KSh -200", -20000},
		{"450.999", 45099},
	}
	for _, tc := range cases {
		got, err := parseMoneyToCents(tc.in)
		if err != nil {
			t.Fatalf("parseMoneyToCents(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("parseMoneyToCents(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}

	for _, raw := range []string{".", "-", "--5", "200-", "2-00", "1.250.50"} {
		if _, err := parseMoneyToCents(raw); err == nil {
			t.Fatalf("parseMoneyToCents(%q) = nil error, want a rejection", raw)
		}
	}
}
