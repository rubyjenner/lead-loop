package main

// Staff replies: a salesperson answers a web customer from the lead page, and the customer's chat page shows
// the answer. Wire it up in main.go:
//
//	registerStaffReplyPublic(app, pool)  // before registerAuthRoutes (public, only when PUBLIC_CHAT_ENABLED=1)
//	registerStaffReplyRoutes(app, pool)  // after registerLeadRoutes (needs login)
//
// Rules:
//   - only for leads of the "web" channel (the public chat) and the "line" channel that are consented, not withdrawn,
//     not anonymized. On LINE the text is pushed to the customer's LINE (the customer's own chat shows it); on the
//     web the customer page polls for it
//   - the text is written by staff, kept in staff_replies, and deleted when the lead is anonymized
//   - the customer page only gets the replies of its own session (session id -> lead), never anything else
//   - every reply is written to the audit log (who, which lead; not the text)

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/jackc/pgx/v5/pgxpool"
)

const staffReplyMaxRunes = 500

func initStaffReplies(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS staff_replies (
		id BIGSERIAL PRIMARY KEY,
		lead_id BIGINT NOT NULL REFERENCES leads(id) ON DELETE CASCADE,
		body TEXT NOT NULL,
		sent_by TEXT NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT now()
	);
	CREATE INDEX IF NOT EXISTS staff_replies_lead_idx ON staff_replies(lead_id, id);
	CREATE TABLE IF NOT EXISTS customer_messages (
		id BIGSERIAL PRIMARY KEY,
		lead_id BIGINT NOT NULL REFERENCES leads(id) ON DELETE CASCADE,
		body TEXT NOT NULL,
		seen_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT now()
	);
	CREATE INDEX IF NOT EXISTS customer_messages_lead_idx ON customer_messages(lead_id, id);
	CREATE OR REPLACE FUNCTION purge_lead_messages() RETURNS trigger AS $$
	BEGIN
		DELETE FROM customer_messages WHERE lead_id = NEW.id;
		DELETE FROM staff_replies WHERE lead_id = NEW.id;
		RETURN NEW;
	END
	$$ LANGUAGE plpgsql;
	DROP TRIGGER IF EXISTS leads_purge_messages ON leads;
	CREATE TRIGGER leads_purge_messages AFTER UPDATE ON leads
		FOR EACH ROW WHEN (NEW.consent_general = FALSE OR NEW.anonymized_at IS NOT NULL)
		EXECUTE FUNCTION purge_lead_messages()`)
	return err
}

type staffReplyRow struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	SentBy    string    `json:"sent_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// POST /leads/:id/reply {"body":"..."}   GET /leads/:id/replies
func registerStaffReplyRoutes(app fiber.Router, pool *pgxpool.Pool) {
	reply := requireRole("sales", "marketing", "admin")

	app.Post("/leads/:id/reply", reply, func(c *fiber.Ctx) error {
		id, err := leadID(c)
		if err != nil {
			return leadErr(c, 400, "invalid lead id")
		}
		var req struct {
			Body string `json:"body"`
		}
		if err := c.BodyParser(&req); err != nil {
			return leadErr(c, 400, "body is required")
		}
		body := strings.TrimSpace(req.Body)
		if body == "" || len([]rune(body)) > staffReplyMaxRunes {
			return leadErr(c, 400, "ข้อความต้องไม่ว่าง และยาวไม่เกิน 500 ตัวอักษร")
		}
		var channel string
		var extID *string
		var ok bool
		err = pool.QueryRow(c.Context(), `SELECT channel, channel_user_id,
			(consent_general AND consent_withdrawn_at IS NULL AND anonymized_at IS NULL) FROM leads WHERE id=$1`, id).Scan(&channel, &extID, &ok)
		if err != nil {
			return leadErr(c, 404, "lead not found")
		}
		if channel != publicChatChannel && channel != lineChannel {
			return leadErr(c, 409, "ตอบได้เฉพาะลีดจากแชตหน้าเว็บหรือ LINE")
		}
		if !ok {
			return leadErr(c, 409, "ลีดนี้ไม่ได้ให้ความยินยอม หรือถอนความยินยอมแล้ว จึงส่งข้อความไม่ได้")
		}
		if channel == lineChannel {
			// LINE: send first, store only if LINE accepted it, so the history never shows a message the customer did not get
			if !lineEnabled() {
				return leadErr(c, 503, "ยังไม่ได้ตั้งค่า LINE ในระบบ")
			}
			if extID == nil || !lineUserIDRe.MatchString(*extID) {
				return leadErr(c, 409, "ลีดนี้ไม่มีรหัสผู้ใช้ LINE ที่ใช้ส่งได้")
			}
			client := &lineClient{token: lineToken(), http: &http.Client{Timeout: 8 * time.Second}}
			if err := client.push(c.Context(), *extID, lineText(body)); err != nil {
				log.Printf("line push for lead %d: %v", id, err) // details stay in the log, not in the response
				return leadErr(c, 502, "ส่งไปที่ LINE ไม่สำเร็จ (ลูกค้าอาจบล็อกบัญชี หรือโควตาข้อความเต็ม) ลองอีกครั้งหรือติดต่อนอกระบบ")
			}
		}
		email := ""
		if u := currentUser(c); u != nil {
			email = u.Email
		}
		var out staffReplyRow
		err = pool.QueryRow(c.Context(), `INSERT INTO staff_replies (lead_id, body, sent_by) VALUES ($1,$2,$3)
			RETURNING id, body, sent_by, created_at`, id, body, email).Scan(&out.ID, &out.Body, &out.SentBy, &out.CreatedAt)
		if err != nil {
			return serverError(c, err)
		}
		auditUser(c, pool, "lead_staff_reply", "lead:"+strconv.FormatInt(id, 10), "")
		return c.Status(201).JSON(out)
	})

	app.Get("/leads/:id/replies", func(c *fiber.Ctx) error {
		id, err := leadID(c)
		if err != nil {
			return leadErr(c, 400, "invalid lead id")
		}
		rows, err := pool.Query(c.Context(), `SELECT id, body, sent_by, created_at FROM staff_replies
			WHERE lead_id=$1 ORDER BY id DESC LIMIT 50`, id)
		if err != nil {
			return serverError(c, err)
		}
		defer rows.Close()
		list := []staffReplyRow{}
		for rows.Next() {
			var r staffReplyRow
			if err := rows.Scan(&r.ID, &r.Body, &r.SentBy, &r.CreatedAt); err != nil {
				return serverError(c, err)
			}
			list = append(list, r)
		}
		return c.JSON(list)
	})

	registerThreadRoute(app, pool)
	registerProductFactsRoutes(app, pool)
	registerProductImageUpload(app, pool) // needs login too, kept here so main.go needs no new line
}

// GET /public/chat/replies?session_id=...&after=12 -> replies newer than id 12 for this customer's own lead
func registerStaffReplyPublic(app fiber.Router, pool *pgxpool.Pool) {
	if err := initStaffReplies(context.Background(), pool); err != nil {
		panic("staff_replies schema: " + err.Error())
	}
	if err := initProductFacts(context.Background(), pool); err != nil {
		panic("product facts schema: " + err.Error())
	}
	pollLimit := limiter.New(limiter.Config{Max: 60, Expiration: time.Minute, LimitReached: func(c *fiber.Ctx) error {
		return c.Status(429).JSON(fiber.Map{"error": "too many requests"})
	}})
	app.Get("/public/chat/replies", pollLimit, func(c *fiber.Ctx) error {
		if !publicChatEnabled() {
			return c.Status(404).JSON(fiber.Map{"error": "not found"})
		}
		sid := c.Query("session_id")
		if !publicSessionRe.MatchString(sid) {
			return leadErr(c, 400, "session_id is required")
		}
		after, _ := strconv.ParseInt(c.Query("after"), 10, 64)
		rows, err := pool.Query(c.Context(), `SELECT r.id, r.body, r.created_at FROM staff_replies r
			JOIN leads l ON l.id = r.lead_id
			WHERE l.channel=$1 AND l.channel_user_id=$2 AND l.anonymized_at IS NULL
			  AND l.consent_general AND l.consent_withdrawn_at IS NULL AND r.id > $3
			ORDER BY r.id LIMIT 20`, publicChatChannel, "pub-"+sid, after)
		if err != nil {
			return serverError(c, err)
		}
		defer rows.Close()
		list := []staffReplyRow{}
		for rows.Next() {
			var r staffReplyRow
			if err := rows.Scan(&r.ID, &r.Body, &r.CreatedAt); err != nil {
				return serverError(c, err)
			}
			list = append(list, r)
		}
		return c.JSON(fiber.Map{"replies": list})
	})
}
