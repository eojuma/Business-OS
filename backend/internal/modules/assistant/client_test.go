package assistant

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/businessos/backend/internal/config"
)

func TestCompleteNormalizesBaseURL(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": "ok"}}},
		})
	}))
	defer srv.Close()

	client := NewAIClient(&config.Config{AIAPIKey: "key", AIBaseURL: srv.URL + "/", AIModel: "m"})
	out, err := client.Complete("hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "ok" {
		t.Fatalf("got %q, want %q", out, "ok")
	}
	if gotPath != "/chat/completions" {
		t.Fatalf("got path %q, want %q (trailing slash not trimmed)", gotPath, "/chat/completions")
	}
}

func TestCompleteRejectsMissingAPIKey(t *testing.T) {
	client := NewAIClient(&config.Config{AIAPIKey: "  ", AIBaseURL: "https://example.com/v1", AIModel: "m"})
	if _, err := client.Complete("hi"); err == nil || !strings.Contains(err.Error(), "AI_API_KEY") {
		t.Fatalf("got err %v, want an AI_API_KEY configuration error", err)
	}
}
