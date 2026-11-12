package main

// LINE channel: customers message the LINE Official Account, LINE calls POST /public/line/webhook.
// Wire it up in main.go next to registerPublicChatRoutes (before registerAuthRoutes):
//
//	registerLineRoutes(app, pool)
//
// It is OFF unless BOTH LINE_CHANNEL_SECRET and LINE_CHANNEL_ACCESS_TOKEN are set. Safeguards:
//   - every request must carry a valid X-Line-Signature (HMAC of the raw body with the channel secret)
//   - only 1:1 chats are handled (groups and rooms are ignored) and the LINE user id must look like "U" + 32 hex
//   - same PDPA rule as every channel: nothing is stored before the customer presses "ยินยอม"; the message
//     text of an unknown person is never read, and their LINE profile is read only after they consent
//   - the customer can withdraw at any time by pressing the button or typing the same words
//   - the reply contains only the chat answer: never the score, level or lead id
//
// New LINE leads are attached to the campaign in LINE_CAMPAIGN_ID (optional; 0 or empty = no campaign).
// A salesperson answers from the lead page (POST /leads/:id/reply pushes the text to the customer's LINE).
// After the lead is handed to people (and until it is closed or lost) the bot stops answering that customer and
// what the customer types is kept for the lead page (see chat_thread.go).

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	lineCreatedBy   = "line-chat"
	lineMaxBodySize = 1 << 20
)

func lineSecret() string { return strings.TrimSpace(os.Getenv("LINE_CHANNEL_SECRET")) }
func lineToken() string  { return strings.TrimSpace(os.Getenv("LINE_CHANNEL_ACCESS_TOKEN")) }
func lineEnabled() bool  { return lineSecret() != "" && lineToken() != "" }

func registerLineRoutes(app fiber.Router, pool *pgxpool.Pool) {
	ch := &chatChannelAPI{leads: &leadAPI{pool: pool}}
	limit := limiter.New(limiter.Config{Max: 300, Expiration: time.Minute,
		LimitReached: func(c *fiber.Ctx) error { return c.SendStatus(429) }})

	app.Post("/public/line/webhook", limit, func(c *fiber.Ctx) error {
		if !lineEnabled() {
			return c.Status(404).JSON(fiber.Map{"error": "not found"})
		}
		body := c.Body()
		if len(body) > lineMaxBodySize {
			return c.SendStatus(413)
		}
		if !lineSignatureValid(lineSecret(), body, c.Get("X-Line-Signature")) {
			return c.SendStatus(401)
		}
		var wh lineWebhook
		if err := json.Unmarshal(body, &wh); err != nil {
			return c.SendStatus(400)
		}
		client := &lineClient{token: lineToken(), http: &http.Client{Timeout: 8 * time.Second}}
		var campaign *int64
		if id := int64(envInt("LINE_CAMPAIGN_ID", 0)); id > 0 {
			campaign = &id
		}
		for _, ev := range wh.Events {
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			if err := ch.handleLineEvent(ctx, client, campaign, ev); err != nil {
				log.Printf("line event %q: %v", ev.Type, err) // log only: LINE must still get 200 or it retries
			}
			cancel()
		}
		return c.SendStatus(200) // an empty "events" list (the Verify button in LINE Developers) also lands here
	})
}

func (ch *chatChannelAPI) handleLineEvent(ctx context.Context, client *lineClient, campaign *int64, ev lineEvent) error {
	if ev.Source.Type != "user" || !lineUserIDRe.MatchString(ev.Source.UserID) {
		return nil
	}
	ext := ev.Source.UserID

	switch ev.Type {
	case "follow": // the person just added the account: greet (consent notice if new)
		res, err := ch.handleIncoming(ctx, lineChannel, ext, "")
		if err != nil {
			return err
		}
		return client.reply(ctx, ev.ReplyToken, lineReply(res))

	case "message":
		if ev.Message.Type != "text" {
			return nil // stickers, images ...: ignored
		}
		text := lineTruncate(strings.TrimSpace(ev.Message.Text), chatMaxMessageText)
		if text == "" {
			return nil
		}
		if text == lineWithdrawCmd {
			if err := ch.withdrawConsent(ctx, lineChannel, ext); err != nil {
				return err
			}
			return client.reply(ctx, ev.ReplyToken, linePlain("ถอนความยินยอมแล้วค่ะ ระบบจะไม่ประมวลผลข้อมูลของคุณอีก หากต้องการคุยต่อ พิมพ์ข้อความมาได้เลย"))
		}
		if lineIsProductCmd(text) {
			if sent, err := ch.offerProducts(ctx, client, ev.ReplyToken, ext, campaign); sent || err != nil {
				return err
			}
		}
		res, err := ch.handleIncoming(ctx, lineChannel, ext, text)
		if err != nil {
			return err
		}
		if res.StaffMode { // a person handles this customer: the text was kept for them, the bot stays quiet
			return nil
		}
		msgs := []lineMessage{lineReply(res)}
		if !res.NeedsConsent && chatHasEvent(res.Detected, "asked_details") { // the bot introduces the product: show its picture
			if u := ch.productPicture(ctx, res.LeadID); u != "" {
				msgs = append(msgs, lineImageMessage(u))
			}
		}
		return client.reply(ctx, ev.ReplyToken, msgs...)

	case "postback":
		if cid, ok := lineProductCampaign(ev.Postback.Data); ok {
			return ch.pickProduct(ctx, client, ev.ReplyToken, ext, cid, campaign)
		}
		switch linePostbackAction(ev.Postback.Data) {
		case "consent":
			name := cleanDisplayName(client.displayName(ctx, ext)) // read only now that the person accepted
			if name == "" {
				name = "ลูกค้า LINE"
			}
			if _, _, err := ch.grantConsent(ctx, lineChannel, ext, name, campaign, false, chatConsentVersion(), lineCreatedBy); err != nil {
				return err
			}
			thanks := lineText(chatReplyConsentThanks)
			if choices, err := ch.productChoices(ctx, campaign); err == nil && len(choices) > 0 {
				thanks = lineProductMenu(chatReplyConsentThanks+"\n\nสนใจสินค้าตัวไหนคะ เลือกได้เลย หรือพิมพ์ถามมาได้ค่ะ", choices)
			}
			return client.reply(ctx, ev.ReplyToken, thanks)
		case "decline":
			return client.reply(ctx, ev.ReplyToken, linePlain("ไม่เป็นไรค่ะ ยังไม่มีการเก็บข้อมูลของคุณ หากเปลี่ยนใจ พิมพ์ข้อความมาได้ทุกเมื่อ"))
		}
	}
	return nil
}

func lineReply(res chatIncomingResult) lineMessage {
	if res.NeedsConsent {
		return lineConsentMessage(res.Reply)
	}
	return lineText(res.Reply)
}

// withdrawConsent is the PDPA withdrawal used by LINE (same effect as POST /public/chat/withdraw).
func (ch *chatChannelAPI) withdrawConsent(ctx context.Context, channel, extID string) error {
	id, found, err := ch.findLead(ctx, channel, extID)
	if err != nil || !found {
		return err
	}
	tag, err := ch.leads.pool.Exec(ctx, `UPDATE leads SET consent_general=FALSE, consent_marketing=FALSE,
		consent_withdrawn_at=now(), updated_at=now() WHERE id=$1 AND consent_general AND consent_withdrawn_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		_, err = ch.leads.pool.Exec(ctx, `INSERT INTO consent_events(lead_id, kind, action, text_version) VALUES($1,'general','withdrawn',$2)`,
			id, chatConsentVersion())
	}
	return err
}

// offerProducts shows the product buttons. sent=false when there is nothing to offer or the person has not
// consented yet (then the normal flow, which asks for consent, runs instead).
func (ch *chatChannelAPI) offerProducts(ctx context.Context, client *lineClient, token, ext string, campaign *int64) (bool, error) {
	id, found, err := ch.findLead(ctx, lineChannel, ext)
	if err != nil || !found {
		return false, err
	}
	if ok, err := ch.leads.leadAllowsProcessing(ctx, id); err != nil || !ok {
		return false, err
	}
	if ch.staffMode(ctx, id) { // a person handles this customer: the bot stays quiet
		return true, nil
	}
	choices, err := ch.productChoices(ctx, campaign)
	if err != nil || len(choices) == 0 {
		return false, err
	}
	return true, client.reply(ctx, token, lineProductMenu("สนใจสินค้าตัวไหนคะ เลือกได้เลยค่ะ", choices))
}

// pickProduct: the customer pressed a product button.
func (ch *chatChannelAPI) pickProduct(ctx context.Context, client *lineClient, token, ext string, campaignID int64, campaign *int64) error {
	id, found, err := ch.findLead(ctx, lineChannel, ext)
	if err != nil {
		return err
	}
	if found {
		if found, err = ch.leads.leadAllowsProcessing(ctx, id); err != nil {
			return err
		}
	}
	if !found { // not consented (or withdrawn): same rule as every message
		res, err := ch.handleIncoming(ctx, lineChannel, ext, "")
		if err != nil {
			return err
		}
		return client.reply(ctx, token, lineReply(res))
	}
	if ch.staffMode(ctx, id) {
		return nil
	}
	ok, err := ch.chooseProduct(ctx, id, campaignID, campaign)
	if err != nil {
		return err
	}
	if !ok {
		return client.reply(ctx, token, linePlain("สินค้านี้ยังไม่เปิดให้เลือกในตอนนี้ค่ะ พิมพ์ \"สินค้า\" เพื่อดูรายการอีกครั้ง"))
	}
	product, err := ch.loadProduct(ctx, id)
	if err != nil {
		return err
	}
	msgs := []lineMessage{lineText(lineIntro(product))}
	if u := ch.productPicture(ctx, id); u != "" {
		msgs = append(msgs, lineImageMessage(u))
	}
	return client.reply(ctx, token, msgs...)
}
