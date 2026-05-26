package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Provider abstracts external LLM providers used by Path B for >limit-row workloads.
type Provider interface {
	Name() string
	Complete(ctx context.Context, model, prompt string) (string, error)
}

// SelectProvider returns the right provider for a model name. Returns ErrNoProvider
// when neither key is set, in which case Path B falls back to DuckDB llm extension.
func SelectProvider(model, openAIKey, anthropicKey string) (Provider, error) {
	m := strings.ToLower(model)
	switch {
	case strings.HasPrefix(m, "gpt") || strings.HasPrefix(m, "o1") || strings.HasPrefix(m, "o3"):
		if openAIKey == "" {
			return nil, ErrNoProvider
		}
		return &OpenAIProvider{APIKey: openAIKey, Client: defaultHTTP()}, nil
	case strings.HasPrefix(m, "claude"):
		if anthropicKey == "" {
			return nil, ErrNoProvider
		}
		return &AnthropicProvider{APIKey: anthropicKey, Client: defaultHTTP()}, nil
	}
	return nil, fmt.Errorf("unknown model %q", model)
}

// ErrNoProvider — caller falls back to DuckDB llm extension.
var ErrNoProvider = errors.New("no external provider configured")

func defaultHTTP() *http.Client { return &http.Client{Timeout: 60 * time.Second} }

// ----- OpenAI -----

type OpenAIProvider struct {
	APIKey string
	Client *http.Client
}

func (p *OpenAIProvider) Name() string { return "openai" }

func (p *OpenAIProvider) Complete(ctx context.Context, model, prompt string) (string, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"max_tokens": 256,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.openai.com/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	req.Header.Set("Content-Type", "application/json")
	res, err := p.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 400 {
		return "", fmt.Errorf("openai %d: %s", res.StatusCode, string(raw))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", errors.New("openai: no choices")
	}
	return parsed.Choices[0].Message.Content, nil
}

// ----- Anthropic -----

type AnthropicProvider struct {
	APIKey string
	Client *http.Client
}

func (p *AnthropicProvider) Name() string { return "anthropic" }

func (p *AnthropicProvider) Complete(ctx context.Context, model, prompt string) (string, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"model":      model,
		"max_tokens": 256,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("x-api-key", p.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")
	res, err := p.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 400 {
		return "", fmt.Errorf("anthropic %d: %s", res.StatusCode, string(raw))
	}
	var parsed struct {
		Content []struct {
			Text string `json:"text"`
			Type string `json:"type"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	for _, c := range parsed.Content {
		if c.Type == "text" {
			return c.Text, nil
		}
	}
	return "", errors.New("anthropic: no text content")
}
