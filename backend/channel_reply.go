package main

// Chat reply logic (pure functions: no DB, no HTTP, so it can be tested alone:
//   go test channel_reply.go channel_reply_test.go).
//
// Guardrail: replies only use facts that exist in the products table. Anything else
// (installments, shipping, discounts) is passed to a person; nothing is invented.

import (
	"strconv"
	"strings"
)

// chatProduct is the product of the lead's campaign (all fields may be empty).
type chatProduct struct {
	Name        string
	Description string
	Price       *float64
	// facts that marketing fills in (product_facts.go); empty = unknown, so the bot hands the question to a person
	Variants        string // รุ่น / ขนาด / สี
	ProductType     string // แบบสินค้า
	AfterSales      string // บริการหลังการขาย
	PaymentMethods  string // ช่องทางชำระเงิน
	InstallmentInfo string // เงื่อนไขการผ่อน
}

const (
	chatReplyUnsub   = "รับทราบค่ะ จากนี้ระบบจะไม่ส่งข้อความโปรโมชันหรือติดตามคุณอีกนะคะ หากมีคำถามเมื่อไหร่ ทักมาได้ตลอดเลยค่ะ"
	chatReplyHandoff = "รับเรื่องไว้แล้วค่ะ เดี๋ยวให้เจ้าหน้าที่ติดต่อกลับโดยเร็วที่สุดนะคะ"
	chatReplyBuy     = "ขอบคุณที่สนใจนะคะ ส่งเรื่องให้ทีมงานยืนยันการสั่งซื้อแล้วค่ะ เจ้าหน้าที่จะติดต่อกลับโดยเร็วที่สุดนะคะ"
	chatReplyGeneric = "ขอบคุณที่ทักมานะคะ ถามเรื่องราคาหรือรายละเอียดสินค้าได้เลยค่ะ หากอยากคุยกับเจ้าหน้าที่ พิมพ์ \"เจ้าหน้าที่\" ได้เลยนะคะ"
	// sent right after the customer accepts the consent notice (web chat, LINE and the mock channel share it)
	chatReplyConsentThanks = "ขอบคุณที่ไว้วางใจนะคะ ตอนนี้ถามเรื่องราคาหรือรายละเอียดสินค้าได้เลยค่ะ หากอยากคุยกับเจ้าหน้าที่ พิมพ์ \"เจ้าหน้าที่\" ได้เลยนะคะ"
)

var chatHandoffEvents = []string{"requested_human", "requested_callback", "requested_visit"}

// ChatConsentPrompt is the first message to someone we do not know yet.
// NOTHING about the customer is stored before they accept it.
// The wording is the team's sample text: the company must review it before real use.
func ChatConsentPrompt(policyURL, retention string) string {
	t := "สวัสดีค่ะ ยินดีต้อนรับค่ะ\n" +
		"ก่อนเริ่มคุย ขอแจ้งการใช้ข้อมูลของคุณนะคะ\n\n" +
		"• เก็บชื่อที่แสดง หัวข้อที่สอบถาม (เช่น ราคา รายละเอียด) และงบประมาณที่คุณแจ้ง (ถ้ามี) เพื่อให้ทีมงานตอบกลับ\n" +
		"• เมื่อเจ้าหน้าที่เข้ามาคุยด้วย ข้อความที่คุณพิมพ์จะถูกเก็บให้เจ้าหน้าที่อ่านจนจบการคุย\n" +
		"• ถอนความยินยอมได้ทุกเมื่อ ข้อความที่เก็บไว้จะถูกลบ\n" +
		"• การรับโปรโมชันจะขอความยินยอมแยกต่างหาก"
	if retention != "" {
		t += "\n\nระยะเวลาเก็บข้อมูล: " + retention
	}
	if policyURL != "" {
		t += "\nนโยบายความเป็นส่วนตัว: " + policyURL
	}
	return t + "\n\nกด \"ยินยอม\" เพื่อเริ่มคุยได้เลยค่ะ"
}

func chatBaht(v float64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	var s string
	if v == float64(int64(v)) {
		s = strconv.FormatInt(int64(v), 10)
	} else {
		s = strconv.FormatFloat(v, 'f', 2, 64)
	}
	intPart, frac := s, ""
	if i := strings.Index(s, "."); i >= 0 {
		intPart, frac = s[:i], s[i:]
	}
	var b strings.Builder
	for i, r := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	out := b.String() + frac
	if neg {
		out = "-" + out
	}
	return out
}

func chatHasEvent(events []string, t string) bool {
	for _, e := range events {
		if e == t {
			return true
		}
	}
	return false
}

func factOf(p *chatProduct, which string) string {
	if p == nil {
		return ""
	}
	switch which {
	case "variants":
		return strings.TrimSpace(p.Variants)
	case "type":
		return strings.TrimSpace(p.ProductType)
	case "after_sales":
		return strings.TrimSpace(p.AfterSales)
	case "payment":
		return strings.TrimSpace(p.PaymentMethods)
	case "installment":
		return strings.TrimSpace(p.InstallmentInfo)
	}
	return ""
}

func chatDefer(topic string) string {
	return "เรื่อง" + topic + " ขอให้ทีมงานตอบให้โดยตรงนะคะ"
}

// ComposeChatReply answers the events detected in the CURRENT message only.
func ComposeChatReply(events []string, p *chatProduct) string {
	if chatHasEvent(events, "unsubscribed") {
		return chatReplyUnsub
	}
	hasName := p != nil && strings.TrimSpace(p.Name) != ""
	desc := ""
	if p != nil {
		desc = strings.TrimSpace(p.Description)
	}
	descShown := false
	var parts []string

	if chatHasEvent(events, "asked_details") {
		switch {
		case hasName && desc != "":
			parts = append(parts, "รายละเอียดของ "+p.Name+" นะคะ\n"+desc)
			descShown = true
		case hasName:
			parts = append(parts, p.Name+" ค่ะ "+chatDefer("รายละเอียดเพิ่มเติม"))
		default:
			parts = append(parts, chatDefer("รายละเอียดสินค้า"))
		}
	}
	if chatHasEvent(events, "asked_price") {
		if hasName && p.Price != nil && *p.Price > 0 {
			parts = append(parts, p.Name+" ราคา "+chatBaht(*p.Price)+" บาทค่ะ")
		} else {
			parts = append(parts, chatDefer("ราคา"))
		}
	}
	if chatHasEvent(events, "asked_condition") && !descShown {
		if desc != "" {
			parts = append(parts, "สภาพสินค้าตามข้อมูลที่มีค่ะ: "+desc)
		} else {
			parts = append(parts, chatDefer("สภาพสินค้า"))
		}
	}
	for _, d := range []struct{ event, topic string }{
		{"asked_shipping", "การจัดส่ง"},
		{"asked_negotiate", "การต่อรองราคา"},
	} {
		if chatHasEvent(events, d.event) {
			parts = append(parts, chatDefer(d.topic))
		}
	}
	// facts marketing filled in: answer with them word for word, otherwise pass the question to a person
	for _, d := range []struct {
		event, topic, label, value string
	}{
		{"asked_variant", "รุ่น ขนาด และสี", "รุ่น/ขนาด/สี: ", factOf(p, "variants")},
		{"asked_type", "แบบสินค้า", "แบบสินค้า: ", factOf(p, "type")},
		{"asked_warranty", "บริการหลังการขาย", "บริการหลังการขาย: ", factOf(p, "after_sales")},
		{"asked_payment", "ช่องทางชำระเงิน", "ชำระเงินได้ด้วย: ", factOf(p, "payment")},
		{"asked_installment", "การผ่อนชำระ", "การผ่อนชำระ: ", factOf(p, "installment")},
	} {
		if !chatHasEvent(events, d.event) {
			continue
		}
		if d.value != "" {
			parts = append(parts, d.label+d.value)
		} else {
			parts = append(parts, chatDefer(d.topic))
		}
	}
	if chatHasEvent(events, "gave_budget") {
		parts = append(parts, "รับทราบงบประมาณแล้วค่ะ")
	}
	if chatHasEvent(events, "ready_to_buy") {
		// the customer wants to order: repeat only what is on file, then a person confirms the order
		if hasName && p.Price != nil && *p.Price > 0 && !chatHasEvent(events, "asked_price") {
			parts = append(parts, p.Name+" ราคา "+chatBaht(*p.Price)+" บาทค่ะ")
		}
		if v := factOf(p, "payment"); v != "" && !chatHasEvent(events, "asked_payment") {
			parts = append(parts, "ชำระเงินได้ด้วย: "+v)
		}
		parts = append(parts, chatReplyBuy)
	} else {
		for _, h := range chatHandoffEvents {
			if chatHasEvent(events, h) {
				parts = append(parts, chatReplyHandoff)
				break
			}
		}
	}
	if len(parts) == 0 {
		return chatReplyGeneric
	}
	return strings.Join(parts, "\n")
}