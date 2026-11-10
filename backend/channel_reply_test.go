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
	for _, want := range []string{"ถอนความยินยอมได้ทุกเมื่อ", "เก็บให้เจ้าหน้าที่อ่าน", "ยินยอม"} {
		if !strings.Contains(p, want) {
			t.Errorf("notice must say %q: %q", want, p)
		}
	}
	p = ChatConsentPrompt("https://example.com/privacy", "90 วัน")
	if !strings.Contains(p, "https://example.com/privacy") || !strings.Contains(p, "90 วัน") {
		t.Errorf("link and retention should appear: %q", p)
	}
}

func TestProductFactsAreQuotedAndMissingOnesAreDeferred(t *testing.T) {
	p := &chatProduct{Name: "เสื้อยืด", Variants: "S M L / ขาว ดำ", ProductType: "คอกลม", AfterSales: "เปลี่ยนไซส์ได้ใน 7 วัน",
		PaymentMethods: "โอน พร้อมเพย์ บัตรเครดิต", InstallmentInfo: "ผ่อน 0% 3 เดือน ผ่านบัตรเครดิต"}
	for _, c := range []struct{ event, want string }{
		{"asked_variant", "S M L / ขาว ดำ"}, {"asked_type", "คอกลม"}, {"asked_warranty", "เปลี่ยนไซส์ได้ใน 7 วัน"},
		{"asked_payment", "โอน พร้อมเพย์ บัตรเครดิต"}, {"asked_installment", "ผ่อน 0% 3 เดือน"},
	} {
		if r := ComposeChatReply([]string{c.event}, p); !strings.Contains(r, c.want) {
			t.Errorf("%s: %q should contain %q", c.event, r, c.want)
		}
	}
	// empty facts: nothing is invented, the question goes to a person
	empty := &chatProduct{Name: "เสื้อยืด"}
	for _, e := range []string{"asked_variant", "asked_type", "asked_warranty", "asked_payment", "asked_installment"} {
		if r := ComposeChatReply([]string{e}, empty); !strings.Contains(r, "ทีมงานตอบให้โดยตรง") {
			t.Errorf("%s with no data must be handed to staff: %q", e, r)
		}
	}
	if r := ComposeChatReply([]string{"asked_payment"}, nil); !strings.Contains(r, "ทีมงานตอบให้โดยตรง") {
		t.Errorf("no product at all: %q", r)
	}
}

func TestReadyToBuy(t *testing.T) {
	p := &chatProduct{Name: "เสื้อยืด", PaymentMethods: "โอน พร้อมเพย์"}
	price := 350.0
	p.Price = &price
	r := ComposeChatReply([]string{"ready_to_buy"}, p)
	for _, want := range []string{"350", "โอน พร้อมเพย์", chatReplyBuy} {
		if !strings.Contains(r, want) {
			t.Errorf("ready_to_buy reply must contain %q: %q", want, r)
		}
	}
	if strings.Contains(r, chatReplyHandoff) {
		t.Errorf("only one hand-off sentence expected: %q", r)
	}
	if r := ComposeChatReply([]string{"ready_to_buy"}, &chatProduct{}); !strings.Contains(r, chatReplyBuy) || strings.Contains(r, "บาท") {
		t.Errorf("without product data nothing is quoted: %q", r)
	}
	if r := ComposeChatReply([]string{"ready_to_buy", "unsubscribed"}, p); r != chatReplyUnsub {
		t.Errorf("unsubscribe still wins: %q", r)
	}
}

func TestNewTopicKeywordsDoNotOvermatch(t *testing.T) {
	kw := map[string][]string{
		"asked_variant": {"รุ่น", "ขนาด", "ไซส์", "size", "สีอะไร", "มีสี", "สีไหน", "กี่สี"},
		"asked_payment": {"ชำระ", "จ่ายยังไง", "จ่ายด้วย", "โอน", "บัตรเครดิต", "พร้อมเพย์", "เก็บเงินปลายทาง"},
		"ready_to_buy":  {"เอาเลย", "สั่งซื้อ", "ขอซื้อ", "ซื้อเลย", "สั่งเลย", "จะซื้อ", "อยากสั่ง", "สั่งยังไง", "ซื้อยังไง"},
	}
	for text, want := range map[string]string{
		"มีสีอะไรบ้างคะ": "asked_variant", "ไซส์ L มีไหม": "asked_variant", "รับบัตรเครดิตไหม": "asked_payment",
		"โอนได้ไหมครับ": "asked_payment", "เอาเลยครับ": "ready_to_buy", "สั่งซื้อยังไงคะ": "ready_to_buy",
	} {
		got := DetectEvents(text, kw)
		found := false
		for _, g := range got {
			if g == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%q should detect %s, got %v", text, want, got)
		}
	}
	// ordinary words that merely contain a letter pair must not trigger a topic
	for _, text := range []string{"เสียงดีไหมคะ", "สวัสดีครับ", "ขอบคุณค่ะ", "ราคาเท่าไหร่"} {
		if got := DetectEvents(text, kw); len(got) > 0 {
			t.Errorf("%q must not trigger %v", text, got)
		}
	}
}

func TestCannedRepliesArePoliteAndFitOneBubble(t *testing.T) {
	for _, r := range []string{chatReplyUnsub, chatReplyHandoff, chatReplyBuy, chatReplyGeneric, chatReplyConsentThanks} {
		if !strings.Contains(r, "ค่ะ") && !strings.Contains(r, "คะ") {
			t.Errorf("not polite Thai: %q", r)
		}
		if n := len([]rune(r)); n > 300 {
			t.Errorf("too long for one chat bubble (%d runes): %q", n, r)
		}
	}
}

func TestPriceAndDetailsReadNaturally(t *testing.T) {
	r := ComposeChatReply([]string{"asked_price"}, chatTestPhone)
	if r != "iPhone 12 ราคา 12,900 บาทค่ะ" {
		t.Errorf("price reply: %q", r)
	}
	r = ComposeChatReply([]string{"asked_details"}, chatTestPhone)
	if !strings.HasPrefix(r, "รายละเอียดของ iPhone 12 นะคะ\n") || !strings.HasSuffix(r, "มือสอง สภาพ 90%") {
		t.Errorf("details reply: %q", r)
	}
}
