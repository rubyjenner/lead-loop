package main

import (
	"strings"
	"testing"
	"time"
)

func TestPayLinkMessage(t *testing.T) {
	exp := time.Date(2026, 11, 18, 7, 30, 0, 0, time.UTC) // 14:30 in Thailand
	m := payLinkMessage("LL000012", "11900.00", "https://shop.example.com/", "tok123", exp)
	for _, want := range []string{"LL000012", "11900.00 บาท", "https://shop.example.com/pay/tok123", "18/11 14:30"} {
		if !strings.Contains(m, want) {
			t.Errorf("message %q lacks %q", m, want)
		}
	}
	if strings.Contains(m, "//pay/") {
		t.Errorf("double slash before /pay in %q", m)
	}
	if n := len([]rune(m)); n > staffReplyMaxRunes {
		t.Errorf("message has %d runes, over the %d reply limit", n, staffReplyMaxRunes)
	}
}
