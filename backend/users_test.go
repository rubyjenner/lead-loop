package main

import (
	"strings"
	"testing"
)

func TestValidEmail(t *testing.T) {
	good := []string{"a@b.co", "first.last@vannessplus.com", "x+tag@example.com"}
	bad := []string{"", "a", "a@b", "a b@c.com", "a@b.com,c@d.com", "<a@b.com>", "Name <a@b.com>", "@b.com", strings.Repeat("a", 250) + "@b.com"}
	for _, e := range good {
		if !validEmail(e) {
			t.Errorf("%q should be valid", e)
		}
	}
	for _, e := range bad {
		if validEmail(e) {
			t.Errorf("%q should be invalid", e)
		}
	}
}

func TestDomainAllowed(t *testing.T) {
	cases := []struct {
		email, list string
		want        bool
	}{
		{"a@x.com", "", true},
		{"a@x.com", "  ", true},
		{"a@x.com", "x.com", true},
		{"a@X.com", "x.com", true},
		{"a@x.com", "y.com, @x.com", true},
		{"a@y.com", "x.com", false},
		{"a@sub.x.com", "x.com", false},
		{"a@evilx.com", "x.com", false},
		{"nodomain", "x.com", false},
	}
	for _, c := range cases {
		if got := domainAllowed(strings.ToLower(c.email), c.list); got != c.want {
			t.Errorf("domainAllowed(%q,%q)=%v want %v", c.email, c.list, got, c.want)
		}
	}
}

func TestTokens(t *testing.T) {
	a, b := randomString(32), randomString(32)
	if a == b || len(a) < 43 {
		t.Fatalf("tokens must be unique and long, got %q %q", a, b)
	}
	if hashToken(a) == hashToken(b) || len(hashToken(a)) != 64 || hashToken(a) != hashToken(a) {
		t.Fatal("hashToken must be deterministic, 64 hex chars, and differ per token")
	}
	if hashToken(a) == a {
		t.Fatal("stored value must not be the raw token")
	}
}
