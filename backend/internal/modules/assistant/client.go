package assistant

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

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
}

func NewAIClient(cfg *config.Config) AIClient {
	return &geminiClient{
		apiKey:  cfg.AIAPIKey,
		baseURL: cfg.AIBaseURL,
		model:   cfg.AIModel,
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
	body, err := json.Marshal(chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ai request failed (%d): %s", resp.StatusCode, string(respBody))
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