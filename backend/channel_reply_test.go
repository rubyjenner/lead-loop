package main

import (
	"strings"
	"testing"
)

func chatFloatPtr(v float64) *float64 { return &v }

var chatTestPhone = &chatProduct{Name: "iPhone 12", Description: "มือสอง สภาพ 90%", Price: chatFloatPtr(12900)}

func TestFormatBaht(t *testing.T) {
	cases := map[float64]string{0: "0", 5: "5", 999: "999", 1000: "1,000", 12900: "12,900", 1234567: "1,234,567", 1999.5: "1,999.50", -1500: "-1,500"}
	for in, want := range cases {
		if got := chatBaht(in); got != want {
			t.Errorf("chatBaht(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestPriceFromProduct(t *testing.T) {
	r := ComposeChatReply([]string{"asked_price"}, chatTestPhone)
	if !strings.Contains(r, "12,900") || !strings.Contains(r, "iPhone 12") {
		t.Errorf("price reply missing product facts: %q", r)
	}
}

func TestPriceWithoutProductGoesToTeam(t *testing.T) {
	for _, p := range []*chatProduct{nil, {Name: "X"}, {Name: "X", Price: chatFloatPtr(0)}, {Price: chatFloatPtr(100)}} {
		r := ComposeChatReply([]string{"asked_price"}, p)
		if strings.Contains(r, "บาท") || !strings.Contains(r, "ทีมงาน") {
			t.Errorf("must not state a price when unknown: %q", r)
		}
	}
}

func TestDetailsAndConditionShareDescription(t *testing.T) {
	r := ComposeChatReply([]string{"asked_details", "asked_condition"}, chatTestPhone)
	if strings.Count(r, "สภาพ 90%") != 1 {
		t.Errorf("description should appear once: %q", r)
	}
}

func TestConditionNeedsDescription(t *testing.T) {
	r := ComposeChatReply([]string{"asked_condition"}, &chatProduct{Name: "X", Price: chatFloatPtr(1)})
	if !strings.Contains(r, "ทีมงาน") {
		t.Errorf("no description: must defer to the team: %q", r)
	}
	r = ComposeChatReply([]string{"asked_condition"}, chatTestPhone)
	if !strings.Contains(r, "สภาพ 90%") {
		t.Errorf("should use the product description: %q", r)
	}
}

func TestNeverInventsInstallmentShippingDiscount(t *testing.T) {
	for _, e := range []string{"asked_installment", "asked_shipping", "asked_negotiate"} {
		r := ComposeChatReply([]string{e}, chatTestPhone)
		if !strings.Contains(r, "ทีมงาน") || strings.ContainsAny(r, "0123456789") {
			t.Errorf("%s must go to the team with no numbers: %q", e, r)
		}
	}
}

func TestHandoffEvents(t *testing.T) {
	for _, e := range chatHandoffEvents {
		if r := ComposeChatReply([]string{e}, chatTestPhone); r != chatReplyHandoff {
			t.Errorf("%s: got %q", e, r)
		}
	}
	r := ComposeChatReply([]string{"asked_price", "requested_callback"}, chatTestPhone)
	if !strings.Contains(r, "12,900") || !strings.HasSuffix(r, chatReplyHandoff) {
		t.Errorf("price answer then handoff note expected: %q", r)
	}
}

func TestUnsubscribedWinsOverEverything(t *testing.T) {
	if r := ComposeChatReply([]string{"asked_price", "unsubscribed", "requested_human"}, chatTestPhone); r != chatReplyUnsub {
		t.Errorf("got %q", r)
	}
}

func TestNoEventGivesGenericReply(t *testing.T) {
	if r := ComposeChatReply(nil, chatTestPhone); r != chatReplyGeneric {
		t.Errorf("got %q", r)
	}
	if r := ComposeChatReply([]string{"unknown_event"}, chatTestPhone); r != chatReplyGeneric {
		t.Errorf("unknown event: got %q", r)
	}
}

func TestOrderIsStable(t *testing.T) {
	a := ComposeChatReply([]string{"asked_price", "asked_details"}, chatTestPhone)
	b := ComposeChatReply([]string{"asked_details", "asked_price"}, chatTestPhone)
	if a != b {
		t.Errorf("reply must not depend on event order:\n%q\n%q", a, b)
	}
}

func TestConsentPrompt(t *testing.T) {
	p := ChatConsentPrompt("", "")
	if strings.Contains(p, "นโยบายความเป็นส่วนตัว:") || strings.Contains(p, "ระยะเวลาเก็บข้อมูล:") {
		t.Errorf("optional parts should be omitted when empty: %q", p)
	}
	p = ChatConsentPrompt("https://example.com/privacy", "90 วัน")
	if !strings.Contains(p, "https://example.com/privacy") || !strings.Contains(p, "90 วัน") {
		t.Errorf("link and retention should appear: %q", p)
	}
}
