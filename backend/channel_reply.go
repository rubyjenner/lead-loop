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
}

const (
	chatReplyUnsub   = "รับทราบค่ะ ระบบจะไม่ส่งข้อความโปรโมชันหรือติดตามคุณอีก หากต้องการสอบถามเพิ่มเติมทักมาได้ทุกเมื่อค่ะ"
	chatReplyHandoff = "ส่งเรื่องให้ทีมงานแล้วค่ะ จะมีเจ้าหน้าที่ติดต่อกลับ"
	chatReplyGeneric = "ขอบคุณที่ทักมาค่ะ ถามราคาหรือรายละเอียดสินค้าได้เลย หรือพิมพ์ \"เจ้าหน้าที่\" หากต้องการคุยกับทีมงาน"
)

var chatHandoffEvents = []string{"requested_human", "requested_callback", "requested_visit"}

// ChatConsentPrompt is the first message to someone we do not know yet.
// NOTHING about the customer is stored before they accept it.
// The wording is the team's sample text: the company must review it before real use.
func ChatConsentPrompt(policyURL, retention string) string {
	t := "สวัสดีค่ะ ก่อนคุยต่อ ขอแจ้งว่าระบบจะเก็บชื่อที่แสดงและประเภทของคำถามที่คุณส่งมา (ไม่เก็บข้อความดิบ) " +
		"เพื่อให้ทีมงานตอบและติดต่อกลับ หากยินยอมกดยินยอมได้เลย และถอนความยินยอมได้ทุกเมื่อ " +
		"ส่วนการรับโปรโมชันจะขอความยินยอมแยกต่างหาก"
	if retention != "" {
		t += " ระยะเวลาเก็บข้อมูล: " + retention
	}
	if policyURL != "" {
		t += " นโยบายความเป็นส่วนตัว: " + policyURL
	}
	return t
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
			parts = append(parts, p.Name+": "+desc)
			descShown = true
		case hasName:
			parts = append(parts, "สินค้าที่สนใจคือ "+p.Name+" "+chatDefer("รายละเอียดเพิ่มเติม"))
		default:
			parts = append(parts, chatDefer("รายละเอียดสินค้า"))
		}
	}
	if chatHasEvent(events, "asked_price") {
		if hasName && p.Price != nil && *p.Price > 0 {
			parts = append(parts, "ราคา "+p.Name+" "+chatBaht(*p.Price)+" บาทค่ะ")
		} else {
			parts = append(parts, chatDefer("ราคา"))
		}
	}
	if chatHasEvent(events, "asked_condition") && !descShown {
		if desc != "" {
			parts = append(parts, "ข้อมูลสภาพสินค้า: "+desc)
		} else {
			parts = append(parts, chatDefer("สภาพสินค้า"))
		}
	}
	for _, d := range []struct{ event, topic string }{
		{"asked_installment", "การผ่อนชำระ"},
		{"asked_shipping", "การจัดส่ง"},
		{"asked_negotiate", "การต่อรองราคา"},
	} {
		if chatHasEvent(events, d.event) {
			parts = append(parts, chatDefer(d.topic))
		}
	}
	for _, h := range chatHandoffEvents {
		if chatHasEvent(events, h) {
			parts = append(parts, chatReplyHandoff)
			break
		}
	}
	if len(parts) == 0 {
		return chatReplyGeneric
	}
	return strings.Join(parts, "\n")
}
