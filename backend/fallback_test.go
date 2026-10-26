package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func price(v float64) *float64 { return &v }

func TestTemplateContentsUseOnlyGivenFacts(t *testing.T) {
	ci := campaignInfo{Name: "Phone sale", Product: "iPhone 12", ProductDesc: "มือสอง สภาพ 90%", Price: price(12900)}
	out := templateContents(ci, "line", 5)
	if len(out) != 5 {
		t.Fatalf("want 5 drafts, got %d", len(out))
	}
	seen := map[string]bool{}
	for _, c := range out {
		if !strings.Contains(c, "iPhone 12") {
			t.Errorf("draft misses product name: %q", c)
		}
		if seen[c] {
			t.Errorf("duplicate draft: %q", c)
		}
		seen[c] = true
	}
	// no price entered -> no price may appear
	for _, c := range templateContents(campaignInfo{Name: "x", Product: "y"}, "line", 5) {
		if strings.Contains(c, "บาท") {
			t.Errorf("invented a price: %q", c)
		}
	}
}

func TestTemplateContentsCount(t *testing.T) {
	ci := campaignInfo{Name: "Launch"}
	if n := len(templateContents(ci, "line", 3)); n != 3 {
		t.Errorf("count 3 -> %d", n)
	}
	if n := len(templateContents(ci, "line", 99)); n != 5 {
		t.Errorf("count 99 should cap at 5, got %d", n)
	}
	if n := len(templateContents(ci, "line", 0)); n != 1 {
		t.Errorf("count 0 should give 1, got %d", n)
	}
}

func TestTemplatePlan(t *testing.T) {
	p := templatePlan(campaignInfo{Name: "Launch", Budget: 10000})
	if !strings.Contains(p, "Launch") || !strings.Contains(p, "10000") {
		t.Errorf("plan misses name or budget: %q", p)
	}
	if !strings.Contains(p, "ยังไม่ได้ระบุ") {
		t.Errorf("plan should say missing data is not specified: %q", p)
	}
}

func fakeAI(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

const claudeOK = `{"content":[{"type":"text","text":"from claude"}]}`
const geminiOK = `{"candidates":[{"content":{"parts":[{"text":"from gemini"}]}}]}`

func setup(t *testing.T, claude, gemini *httptest.Server) {
	t.Helper()
	oldC, oldG, oldD := claudeURL, geminiBase, retryDelay
	claudeURL, geminiBase, retryDelay = claude.URL, gemini.URL+"/", time.Millisecond
	t.Cleanup(func() { claudeURL, geminiBase, retryDelay = oldC, oldG, oldD; claude.Close(); gemini.Close() })
	t.Setenv("ANTHROPIC_API_KEY", "ck")
	t.Setenv("GEMINI_API_KEY", "gk")
	t.Setenv("AI_PROVIDERS", "")
}

func runR(allow bool) Result {
	return generate(t0(), "sys", "user", allow, func() string { return "from template" })
}

func run() (string, string) {
	r := runR(true)
	return r.Text, r.By
}

func TestClaudeFirst(t *testing.T) {
	setup(t, fakeAI(t, 200, claudeOK), fakeAI(t, 200, geminiOK))
	if text, by := run(); text != "from claude" || by != "claude" {
		t.Errorf("got %q by %q", text, by)
	}
}

func TestFallsBackToGemini(t *testing.T) {
	setup(t, fakeAI(t, 500, "boom"), fakeAI(t, 200, geminiOK))
	if text, by := run(); text != "from gemini" || by != "gemini" {
		t.Errorf("got %q by %q", text, by)
	}
}

func TestRateLimitedClaudeFallsBack(t *testing.T) {
	setup(t, fakeAI(t, 429, "slow down"), fakeAI(t, 200, geminiOK))
	if _, by := run(); by != "gemini" {
		t.Errorf("by %q", by)
	}
}

func TestBadKeyDoesNotRetryButFallsBack(t *testing.T) {
	calls := 0
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(401)
	}))
	setup(t, bad, fakeAI(t, 200, geminiOK))
	if _, by := run(); by != "gemini" {
		t.Errorf("by %q", by)
	}
	if calls != 1 {
		t.Errorf("401 must not be retried, calls=%d", calls)
	}
}

func TestAllProvidersFailUsesTemplate(t *testing.T) {
	setup(t, fakeAI(t, 500, ""), fakeAI(t, 503, ""))
	if text, by := run(); text != "from template" || by != "template" {
		t.Errorf("got %q by %q", text, by)
	}
}

func TestEmptyAnswerFallsBack(t *testing.T) {
	setup(t, fakeAI(t, 200, `{"content":[]}`), fakeAI(t, 200, `{"candidates":[]}`))
	if _, by := run(); by != "template" {
		t.Errorf("by %q", by)
	}
}

func TestNoKeysUsesTemplateWithoutNetwork(t *testing.T) {
	setup(t, fakeAI(t, 200, claudeOK), fakeAI(t, 200, geminiOK))
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	if _, by := run(); by != "template" {
		t.Errorf("by %q", by)
	}
}

func TestProviderOrderFromEnv(t *testing.T) {
	setup(t, fakeAI(t, 200, claudeOK), fakeAI(t, 200, geminiOK))
	t.Setenv("AI_PROVIDERS", "gemini, claude")
	if _, by := run(); by != "gemini" {
		t.Errorf("gemini should be first, got %q", by)
	}
	t.Setenv("AI_PROVIDERS", "template")
	if _, by := run(); by != "template" {
		t.Errorf("template only, got %q", by)
	}
}

func t0() context.Context { return context.Background() }

func TestUsageTokensAreReported(t *testing.T) {
	setup(t,
		fakeAI(t, 200, `{"content":[{"type":"text","text":"from claude"}],"usage":{"input_tokens":12,"output_tokens":34}}`),
		fakeAI(t, 200, geminiOK))
	if r := runR(true); r.In != 12 || r.Out != 34 || r.By != "claude" {
		t.Errorf("claude usage: %+v", r)
	}
	t.Setenv("AI_PROVIDERS", "gemini")
	g := fakeAI(t, 200, `{"candidates":[{"content":{"parts":[{"text":"hi"}]}}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":6}}`)
	oldG := geminiBase
	geminiBase = g.URL + "/"
	defer func() { geminiBase = oldG; g.Close() }()
	if r := runR(true); r.In != 5 || r.Out != 6 || r.By != "gemini" {
		t.Errorf("gemini usage: %+v", r)
	}
}

func TestQuotaReachedSkipsExternalAI(t *testing.T) {
	calls := 0
	spy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; _, _ = w.Write([]byte(claudeOK)) }))
	setup(t, spy, fakeAI(t, 200, geminiOK))
	r := runR(false)
	if r.By != "template" || r.Reason != "quota" || r.Text != "from template" || calls != 0 {
		t.Errorf("quota: %+v calls=%d", r, calls)
	}
}

func TestReasonDistinguishesNoKeyFromOutage(t *testing.T) {
	setup(t, fakeAI(t, 500, ""), fakeAI(t, 500, ""))
	if r := runR(true); r.By != "template" || r.Reason != "unavailable" {
		t.Errorf("outage: %+v", r)
	}
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	if r := runR(true); r.By != "template" || r.Reason != "no_key" {
		t.Errorf("no key: %+v", r)
	}
}

func TestSplitDrafts(t *testing.T) {
	got := splitDrafts("one\n---\ntwo\n---\n\n---\nthree\n---\nfour", 3)
	if len(got) != 3 || got[0] != "one" || got[2] != "three" {
		t.Errorf("got %q", got)
	}
}
