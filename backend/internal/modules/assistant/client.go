package assistant

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/businessos/backend/internal/config"
)

// AIClient is the only thing this module knows about talking to an LLM —
// "send a prompt, get text back." Nothing else in the codebase should
// import an AI SDK directly; everything funnels through this.
type AIClient interface {
	Complete(prompt string) (string, error)
}

type geminiClient struct {
	apiKey  string
	baseURL string
	model   string
	http    *http.Client
}

func NewAIClient(cfg *config.Config) AIClient {
	return &geminiClient{
		apiKey:  strings.TrimSpace(cfg.AIAPIKey),
		baseURL: strings.TrimRight(strings.TrimSpace(cfg.AIBaseURL), "/"),
		model:   cfg.AIModel,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func (c *geminiClient) Complete(prompt string) (string, error) {
	if c.apiKey == "" {
		return "", fmt.Errorf("AI_API_KEY is not configured")
	}
	if c.baseURL == "" {
		return "", fmt.Errorf("AI_BASE_URL is not configured")
	}

	body, err := json.Marshal(chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return "", err
	}

	endpoint := c.baseURL + "/chat/completions"
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("ai request to %s failed: %w", endpoint, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ai request to %s with model %q failed (%d): %s", endpoint, c.model, resp.StatusCode, string(respBody))
	}

	var parsed chatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("ai returned no choices")
	}

	return parsed.Choices[0].Message.Content, nil
}
