package main

// Public customer chat (no login): a test customer opens /chat, accepts the consent notice and talks to the
// system, exactly like a customer on LINE would. Wire it up in main.go BEFORE registerAuthRoutes:
//
//	registerPublicChatRoutes(app, pool)
//
// It is OFF unless PUBLIC_CHAT_ENABLED=1, because it lets anyone on the internet create a lead. Safeguards:
//   - per-IP rate limits (messages and consent)
//   - a cap on new leads per hour (PUBLIC_CHAT_MAX_LEADS_PER_HOUR, default 100)
//   - the customer's session id is a random string the page makes (16-64 chars); the server prefixes it and
//     stores it as channel "web", so it can never collide with team test chats
//   - answers contain only the chat reply: never the score, level, lead id or any other customer's data
//   - nothing is stored before the customer accepts the consent notice (same rule as every channel)
//   - the public campaign info is limited to the campaign name and its product name and price

import (
	"context"
	"errors"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	publicChatChannel   = "web"
	publicChatCreatedBy = "public-chat"
)

var publicSessionRe = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)

func publicChatEnabled() bool { return os.Getenv("PUBLIC_CHAT_ENABLED") == "1" }

func cleanDisplayName(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > 60 {
		s = string(r[:60])
	}
	return s
}

func registerPublicChatRoutes(app fiber.Router, pool *pgxpool.Pool) {
	ch := &chatChannelAPI{leads: &leadAPI{pool: pool}}
	maxPerHour := envInt("PUBLIC_CHAT_MAX_LEADS_PER_HOUR", 100)

	gate := func(c *fiber.Ctx) error {
		if !publicChatEnabled() {
			return c.Status(404).JSON(fiber.Map{"error": "not found"})
		}
		return c.Next()
	}
	tooMany := func(c *fiber.Ctx) error {
		return c.Status(429).JSON(fiber.Map{"error": "ส่งข้อความเร็วเกินไป กรุณารอสักครู่แล้วลองใหม่"})
	}
	msgLimit := limiter.New(limiter.Config{Max: 40, Expiration: time.Minute, LimitReached: tooMany})
	consentLimit := limiter.New(limiter.Config{Max: 8, Expiration: time.Minute, LimitReached: tooMany})
	infoLimit := limiter.New(limiter.Config{Max: 60, Expiration: time.Minute, LimitReached: tooMany})

	// the page asks this first: is the public chat on, and what is it about
	app.Get("/public/chat/info", infoLimit, func(c *fiber.Ctx) error {
		if !publicChatEnabled() {
			return c.JSON(fiber.Map{"enabled": false})
		}
		out := fiber.Map{"enabled": true}
		if id, err := strconv.ParseInt(c.Query("campaign_id"), 10, 64); err == nil && id > 0 {
			var camp string
			var prod *string
			var price *float64
			err := pool.QueryRow(c.Context(), `SELECT c.name, p.name, p.price::float8 FROM campaigns c
				LEFT JOIN products p ON p.id = c.product_id WHERE c.id=$1`, id).Scan(&camp, &prod, &price)
			if err == nil {
				out["campaign_name"], out["product_name"], out["product_price"] = camp, prod, price
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return serverError(c, err)
			}
		}
		return c.JSON(out)
	})

	// POST /public/chat/messages {"session_id":"...","text":"..."}
	app.Post("/public/chat/messages", gate, msgLimit, func(c *fiber.Ctx) error {
		var req struct {
			SessionID string `json:"session_id"`
			Text      string `json:"text"`
		}
		if err := c.BodyParser(&req); err != nil || !publicSessionRe.MatchString(req.SessionID) || strings.TrimSpace(req.Text) == "" {
			return leadErr(c, 400, "session_id and text are required")
		}
		if len([]rune(req.Text)) > 500 {
			return leadErr(c, 400, "ข้อความยาวเกินไป")
		}
		res, err := ch.handleIncoming(c.Context(), publicChatChannel, "pub-"+req.SessionID, req.Text)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(fiber.Map{"reply": res.Reply, "needs_consent": res.NeedsConsent})
	})

	// POST /public/chat/consent {"session_id":"...","display_name":"...","campaign_id":1,"consent_marketing":false}
	app.Post("/public/chat/consent", gate, consentLimit, func(c *fiber.Ctx) error {
		var req struct {
			SessionID        string `json:"session_id"`
			DisplayName      string `json:"display_name"`
			CampaignID       *int64 `json:"campaign_id"`
			ConsentMarketing bool   `json:"consent_marketing"`
		}
		if err := c.BodyParser(&req); err != nil || !publicSessionRe.MatchString(req.SessionID) {
			return leadErr(c, 400, "session_id is required")
		}
		ext := "pub-" + req.SessionID
		if _, found, err := ch.findLead(c.Context(), publicChatChannel, ext); err != nil {
			return serverError(c, err)
		} else if !found { // only brand-new people count against the hourly cap
			var n int
			if err := pool.QueryRow(c.Context(), `SELECT count(*) FROM leads WHERE created_by=$1 AND created_at > now() - interval '1 hour'`,
				publicChatCreatedBy).Scan(&n); err != nil {
				return serverError(c, err)
			}
			if n >= maxPerHour {
				return c.Status(429).JSON(fiber.Map{"error": "ระบบทดลองรับผู้ใช้ใหม่เต็มชั่วคราว กรุณาลองใหม่ภายหลัง"})
			}
		}
		name := cleanDisplayName(req.DisplayName)
		if name == "" {
			name = "ลูกค้าเว็บ"
		}
		_, _, err := ch.grantConsent(c.Context(), publicChatChannel, ext, name, req.CampaignID,
			req.ConsentMarketing, chatConsentVersion(), publicChatCreatedBy)
		if errors.Is(err, errChatCampaignNotFound) {
			return leadErr(c, 404, "campaign not found")
		}
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(fiber.Map{"reply": "ขอบคุณที่ยินยอมค่ะ ถามราคาหรือรายละเอียดสินค้าได้เลย"})
	})

	// POST /public/chat/withdraw {"session_id":"..."}: the customer withdraws consent (PDPA); nothing more is processed
	app.Post("/public/chat/withdraw", gate, consentLimit, func(c *fiber.Ctx) error {
		var req struct {
			SessionID string `json:"session_id"`
		}
		if err := c.BodyParser(&req); err != nil || !publicSessionRe.MatchString(req.SessionID) {
			return leadErr(c, 400, "session_id is required")
		}
		id, found, err := ch.findLead(c.Context(), publicChatChannel, "pub-"+req.SessionID)
		if err != nil {
			return serverError(c, err)
		}
		if found {
			tag, err := pool.Exec(c.Context(), `UPDATE leads SET consent_general=FALSE, consent_marketing=FALSE,
				consent_withdrawn_at=now(), updated_at=now() WHERE id=$1 AND consent_general AND consent_withdrawn_at IS NULL`, id)
			if err != nil {
				return serverError(c, err)
			}
			if tag.RowsAffected() > 0 {
				if _, err := pool.Exec(c.Context(), `INSERT INTO consent_events(lead_id, kind, action, text_version) VALUES($1,'general','withdrawn',$2)`,
					id, chatConsentVersion()); err != nil {
					return serverError(c, err)
				}
			}
		}
		return c.JSON(fiber.Map{"ok": true, "reply": "ถอนความยินยอมแล้วค่ะ ระบบจะไม่ประมวลผลข้อมูลของคุณอีก"})
	})
}

// ---- public storefront: what a customer sees before chatting ----
// Only approved content is shown, so nothing reaches customers without a human approval (approver != creator).

func initShop(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `ALTER TABLE products ADD COLUMN IF NOT EXISTS image_url TEXT NOT NULL DEFAULT ''`)
	return err
}

func validImageURL(s string) bool {
	s = strings.TrimSpace(s)
	return len(s) <= 500 && (strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://"))
}

// saveProductImage reads an optional "image_url" from the same request body that created the product.
func saveProductImage(c *fiber.Ctx, pool *pgxpool.Pool, productID int64) {
	var in struct {
		ImageURL string `json:"image_url"`
	}
	if c.BodyParser(&in) != nil || !validImageURL(in.ImageURL) {
		return
	}
	_, _ = pool.Exec(c.Context(), `UPDATE products SET image_url=$1 WHERE id=$2`, strings.TrimSpace(in.ImageURL), productID)
}

func registerShopRoutes(app fiber.Router, pool *pgxpool.Pool) {
	if err := initShop(context.Background(), pool); err != nil {
		log.Fatalf("shop schema: %v", err)
	}
	infoLimit := limiter.New(limiter.Config{Max: 60, Expiration: time.Minute})
	// GET /public/shop?campaign_id=1 -> campaign, product (name, description, price, image) and approved posts
	app.Get("/public/shop", infoLimit, func(c *fiber.Ctx) error {
		if !publicChatEnabled() {
			return c.Status(404).JSON(fiber.Map{"error": "not found"})
		}
		id, err := strconv.ParseInt(c.Query("campaign_id"), 10, 64)
		if err != nil || id <= 0 {
			return c.Status(400).JSON(fiber.Map{"error": "campaign_id is required"})
		}
		var camp string
		var pname, pdesc, pimg *string
		var price *float64
		err = pool.QueryRow(c.Context(), `SELECT c.name, p.name, p.description, p.price::float8, p.image_url
			FROM campaigns c LEFT JOIN products p ON p.id = c.product_id WHERE c.id=$1`, id).Scan(&camp, &pname, &pdesc, &price, &pimg)
		if errors.Is(err, pgx.ErrNoRows) {
			return c.Status(404).JSON(fiber.Map{"error": "not found"})
		}
		if err != nil {
			return serverError(c, err)
		}
		rows, err := pool.Query(c.Context(), `SELECT body FROM contents
			WHERE campaign_id=$1 AND status::text IN ('approved','published') ORDER BY approved_at DESC NULLS LAST, id DESC LIMIT 3`, id)
		if err != nil {
			return serverError(c, err)
		}
		defer rows.Close()
		posts := []string{}
		for rows.Next() {
			var b string
			if err := rows.Scan(&b); err != nil {
				return serverError(c, err)
			}
			posts = append(posts, b)
		}
		return c.JSON(fiber.Map{"enabled": true, "campaign_name": camp, "product_name": pname, "description": pdesc,
			"product_price": price, "image_url": pimg, "posts": posts})
	})
}
