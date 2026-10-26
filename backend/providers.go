package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// Only the standard library is used here so the fallback chain can be unit tested on its own.

var (
	httpClient = &http.Client{Timeout: 30 * time.Second}
	claudeURL  = "https://api.anthropic.com/v1/messages"
	geminiBase = "https://generativelanguage.googleapis.com/v1beta/models/"
	retryDelay = 2 * time.Second // wait before a retry; tests set it to 0
)

// Usage is the token count reported by the provider (0 for the template fallback).
type Usage struct{ In, Out int }

const (
	maxAttempts     = 2                // per provider: 1 try + 1 retry on network error, 429 or 5xx
	providerTimeout = 45 * time.Second // hard cap per provider so a slow provider cannot block the fallback
)

// postJSON sends a JSON POST. It retries on network errors, 429 and 5xx; any other non-200 status
// fails at once. Errors only carry the status code, never the prompt (it may contain customer data).
func postJSON(ctx context.Context, url string, headers map[string]string, body []byte) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(retryDelay * time.Duration(attempt)):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("content-type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("status %d", resp.StatusCode)
		}
		return data, nil
	}
	return nil, fmt.Errorf("unavailable after retries: %w", lastErr)
}

// callClaude calls the Claude Messages API.
func callClaude(ctx context.Context, key, system, user string) (string, Usage, error) {
	model := os.Getenv("ANTHROPIC_MODEL")
	if model == "" {
		model = "claude-sonnet-5-5"
	}
	body, err := json.Marshal(map[string]any{
		"model":      model,
		"max_tokens": 1500,
		"system":     system,
		"messages":   []map[string]string{{"role": "user", "content": user}},
	})
	if err != nil {
		return "", Usage{}, err
	}
	data, err := postJSON(ctx, claudeURL,
		map[string]string{"x-api-key": key, "anthropic-version": "2023-06-01"}, body)
	if err != nil {
		return "", Usage{}, err
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", Usage{}, err
	}
	var sb strings.Builder
	for _, b := range out.Content {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	return strings.TrimSpace(sb.String()), Usage{In: out.Usage.InputTokens, Out: out.Usage.OutputTokens}, nil
}

// callGemini calls the Gemini generateContent API. The key goes in a header, never in the URL.
func callGemini(ctx context.Context, key, system, user string) (string, Usage, error) {
	model := os.Getenv("GEMINI_MODEL")
	if model == "" {
		model = "gemini-2.5-flash"
	}
	body, err := json.Marshal(map[string]any{
		"systemInstruction": map[string]any{"parts": []map[string]string{{"text": system}}},
		"contents":          []map[string]any{{"role": "user", "parts": []map[string]string{{"text": user}}}},
		"generationConfig":  map[string]any{"maxOutputTokens": 1500},
	})
	if err != nil {
		return "", Usage{}, err
	}
	data, err := postJSON(ctx, geminiBase+model+":generateContent",
		map[string]string{"x-goog-api-key": key}, body)
	if err != nil {
		return "", Usage{}, err
	}
	var out struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		UsageMetadata struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", Usage{}, err
	}
	var sb strings.Builder
	for _, c := range out.Candidates {
		for _, p := range c.Content.Parts {
			sb.WriteString(p.Text)
		}
		break // first candidate only
	}
	return strings.TrimSpace(sb.String()), Usage{In: out.UsageMetadata.PromptTokenCount, Out: out.UsageMetadata.CandidatesTokenCount}, nil
}

// providerOrder reads AI_PROVIDERS (default "claude,gemini"). "template" is not listed: the rule/template
// fallback always runs last. Set AI_PROVIDERS=template to turn external AI off completely.
func providerOrder() []string {
	raw := os.Getenv("AI_PROVIDERS")
	if raw == "" {
		raw = "claude,gemini"
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "claude" || p == "gemini" {
			out = append(out, p)
		}
	}
	return out
}

// Result says what produced a text. By is "claude", "gemini" or "template" ("" from tryProviders when
// nothing external worked). Reason explains a template result: "quota" (daily limit reached, external AI
// skipped), "no_key" (no provider configured) or "unavailable" (providers were tried and failed).
type Result struct {
	Text, By, Reason string
	In, Out          int
}

// tryProviders tries each configured provider in order. A provider without an API key is skipped.
// It returns By == "" when none succeeded.
func tryProviders(ctx context.Context, system, user string) Result {
	configured := false
	for _, p := range providerOrder() {
		var key string
		var call func(context.Context, string, string, string) (string, Usage, error)
		switch p {
		case "claude":
			key, call = os.Getenv("ANTHROPIC_API_KEY"), callClaude
		case "gemini":
			key, call = os.Getenv("GEMINI_API_KEY"), callGemini
		}
		if key == "" {
			continue
		}
		configured = true
		pctx, cancel := context.WithTimeout(ctx, providerTimeout)
		t, u, err := call(pctx, key, system, user)
		cancel()
		if err != nil {
			log.Printf("ai provider %s failed: %v", p, err)
			continue
		}
		if t == "" {
			log.Printf("ai provider %s returned an empty answer", p)
			continue
		}
		return Result{Text: t, By: p, In: u.In, Out: u.Out}
	}
	if !configured {
		return Result{Reason: "no_key"}
	}
	return Result{Reason: "unavailable"}
}

// generate returns AI text, or the rule/template fallback. It never fails. With allowExternal=false
// (daily quota reached) no external provider is called.
func generate(ctx context.Context, system, user string, allowExternal bool, fallback func() string) Result {
	if !allowExternal {
		return Result{Text: fallback(), By: "template", Reason: "quota"}
	}
	r := tryProviders(ctx, system, user)
	if r.By != "" {
		return r
	}
	r.Text, r.By = fallback(), "template"
	return r
}
