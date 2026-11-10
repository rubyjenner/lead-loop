package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// capture starts a fake provider that records the JSON body it receives and answers with the given reply.
func capture(t *testing.T, reply string) (*httptest.Server, *map[string]any) {
	t.Helper()
	got := map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

var testImg = &aiImage{Mime: "image/png", B64: "QUJD"}

func TestClaudeGetsThePictureBeforeTheText(t *testing.T) {
	srv, got := capture(t, `{"content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":5,"output_tokens":2}}`)
	old := claudeURL
	claudeURL = srv.URL
	defer func() { claudeURL = old }()

	text, _, err := callClaudeImg(context.Background(), "k", "SYS", "write a post", testImg)
	if err != nil || text != "ok" {
		t.Fatalf("text=%q err=%v", text, err)
	}
	msgs := (*got)["messages"].([]any)
	content := msgs[0].(map[string]any)["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("want image + text blocks, got %v", content)
	}
	img := content[0].(map[string]any)
	src := img["source"].(map[string]any)
	if img["type"] != "image" || src["type"] != "base64" || src["media_type"] != "image/png" || src["data"] != "QUJD" {
		t.Errorf("bad image block: %v", img)
	}
	if content[1].(map[string]any)["text"] != "write a post" {
		t.Errorf("text block wrong: %v", content[1])
	}
	if sys := (*got)["system"].(string); !strings.HasPrefix(sys, "SYS") || !strings.Contains(sys, "ห้ามเดา") {
		t.Errorf("system prompt must keep the rules and add the picture note: %q", sys)
	}
}

func TestClaudeWithoutPictureIsUnchanged(t *testing.T) {
	srv, got := capture(t, `{"content":[{"type":"text","text":"ok"}]}`)
	old := claudeURL
	claudeURL = srv.URL
	defer func() { claudeURL = old }()

	if _, _, err := callClaude(context.Background(), "k", "SYS", "hello"); err != nil {
		t.Fatal(err)
	}
	msgs := (*got)["messages"].([]any)
	if c := msgs[0].(map[string]any)["content"]; c != "hello" {
		t.Errorf("without a picture the content stays a plain string, got %v", c)
	}
	if (*got)["system"] != "SYS" {
		t.Errorf("system prompt must be untouched: %v", (*got)["system"])
	}
}

func TestGeminiGetsThePictureBeforeTheText(t *testing.T) {
	srv, got := capture(t, `{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}`)
	old := geminiBase
	geminiBase = srv.URL + "/"
	defer func() { geminiBase = old }()

	text, _, err := callGeminiImg(context.Background(), "k", "SYS", "write a post", testImg)
	if err != nil || text != "ok" {
		t.Fatalf("text=%q err=%v", text, err)
	}
	parts := (*got)["contents"].([]any)[0].(map[string]any)["parts"].([]any)
	if len(parts) != 2 {
		t.Fatalf("want picture + text parts, got %v", parts)
	}
	in := parts[0].(map[string]any)["inline_data"].(map[string]any)
	if in["mime_type"] != "image/png" || in["data"] != "QUJD" {
		t.Errorf("bad inline_data: %v", in)
	}
	if parts[1].(map[string]any)["text"] != "write a post" {
		t.Errorf("text part wrong: %v", parts[1])
	}
}

func TestTemplateIgnoresPictureAndImageUsedIsHonest(t *testing.T) {
	// no keys configured -> the template writes the text; the picture never reached an AI, so it must not be reported as used
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	r := generateImg(context.Background(), "s", "u", true, func() string { return "tmpl" }, testImg)
	if r.By != "template" || r.Text != "tmpl" {
		t.Fatalf("r = %+v", r)
	}
	if got := imageUsed(r, "upload"); got != "" {
		t.Errorf("imageUsed = %q for a template result, want empty", got)
	}
	if got := imageUsed(Result{By: "claude"}, "product"); got != "product" {
		t.Errorf("imageUsed = %q, want product", got)
	}
}
