package main

// Who is responsible for a lead, and who gets told when a lead is ready for sales.
//
//	GET  /lead-assignments            who owns which lead (login): [{lead_id, email, name}]
//	PUT  /leads/:id/assignment        {"assignee":"sales@x.com"} or {"assignee":""} to release
//	                                  sales: may take an unassigned lead for themselves or release their own
//	                                  admin: may give a lead to any active sales/admin user
//	GET  /sales-team                  active sales users an admin can pick from (admin)
//
// A lead that reaches the hand-off threshold is given automatically to the active salesperson with the fewest open
// leads (AUTO_ASSIGN=0 turns this off; admins and sales can always change it). Taking a lead (status handed_off)
// assigns it to whoever took it when nobody owns it yet.
//
// NOTIFY_WEBHOOK_URL (optional): a POST {"text": "..."} (Slack/Discord/Teams style) is sent when a lead becomes ready.
// The text holds only the lead number, score and owner, never the customer's name or messages.
//
// Wire-up: initAssignment runs from registerStaffReplyPublic, the routes from registerStaffReplyRoutes.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

func initAssignment(ctx context.Context, pool *pgxpool.Pool) error {
	for _, s := range []string{
		`CREATE TABLE IF NOT EXISTS seed_flags (key TEXT PRIMARY KEY, created_at TIMESTAMPTZ NOT NULL DEFAULT now())`,
		`ALTER TABLE leads ADD COLUMN IF NOT EXISTS assigned_to TEXT`,
		`ALTER TABLE leads ADD COLUMN IF NOT EXISTS assigned_at TIMESTAMPTZ`,
		`CREATE INDEX IF NOT EXISTS idx_leads_assigned_to ON leads(assigned_to) WHERE assigned_to IS NOT NULL`,
	} {
		if _, err := pool.Exec(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

func autoAssignEnabled() bool { return os.Getenv("AUTO_ASSIGN") != "0" }

// autoAssign gives an unowned, qualified lead to the active salesperson with the fewest open leads
// (ties: the one who has waited longest for a lead). It never changes an existing owner. Returns the owner or "".
func autoAssign(ctx context.Context, pool *pgxpool.Pool, leadID int64) string {
	if !autoAssignEnabled() {
		return ""
	}
	var email string
	err := pool.QueryRow(ctx, `
		WITH pick AS (
		  SELECT u.email FROM users u
		  WHERE u.role='sales' AND u.active
		  ORDER BY (SELECT count(*) FROM leads l WHERE l.assigned_to=u.email AND l.status IN ('qualified','handed_off') AND l.anonymized_at IS NULL),
		           COALESCE((SELECT max(assigned_at) FROM leads l WHERE l.assigned_to=u.email), 'epoch'::timestamptz), u.id
		  LIMIT 1)
		UPDATE leads SET assigned_to=(SELECT email FROM pick), assigned_at=now()
		WHERE id=$1 AND assigned_to IS NULL AND anonymized_at IS NULL AND EXISTS (SELECT 1 FROM pick)
		RETURNING assigned_to`, leadID).Scan(&email)
	if err != nil {
		return "" // no rows = already owned, or no salesperson to give it to
	}
	return email
}

// notifyLeadReady posts a short, personal-data-free message to NOTIFY_WEBHOOK_URL (if set). Never blocks the caller.
func notifyLeadReady(leadID int64, score int, owner string) {
	url := os.Getenv("NOTIFY_WEBHOOK_URL")
	if url == "" {
		return
	}
	who := "ยังไม่มีผู้รับผิดชอบ"
	if owner != "" {
		who = "ผู้รับผิดชอบ " + owner
	}
	body, _ := json.Marshal(map[string]string{"text": fmt.Sprintf("LeadLoop: ลีด #%d พร้อมส่งต่อ (คะแนน %d) %s", leadID, score, who)})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("notify webhook: %v", err)
			return
		}
		resp.Body.Close()
	}()
}

// onLeadQualified is called when a chat message pushed a lead over the threshold.
func onLeadQualified(ctx context.Context, pool *pgxpool.Pool, leadID int64, score int) {
	var already bool
	_ = pool.QueryRow(ctx, `SELECT assigned_to IS NOT NULL FROM leads WHERE id=$1`, leadID).Scan(&already)
	owner := ""
	if !already {
		owner = autoAssign(ctx, pool, leadID)
	}
	var notified bool // tell once per lead: a marker row in seed_flags
	if err := pool.QueryRow(ctx, `INSERT INTO seed_flags(key) VALUES ($1) ON CONFLICT DO NOTHING RETURNING true`,
		fmt.Sprintf("notified_lead_%d", leadID)).Scan(&notified); err == nil && notified {
		notifyLeadReady(leadID, score, owner)
	}
}

func registerAssignmentRoutes(app fiber.Router, pool *pgxpool.Pool) {
	staff := requireRole("sales", "admin")
	admin := requireRole("admin")

	app.Get("/lead-assignments", func(c *fiber.Ctx) error {
		rows, err := pool.Query(c.Context(), `SELECT l.id, l.assigned_to, COALESCE(NULLIF(u.name,''), l.assigned_to)
			FROM leads l LEFT JOIN users u ON u.email=l.assigned_to
			WHERE l.assigned_to IS NOT NULL AND l.anonymized_at IS NULL`)
		if err != nil {
			return serverError(c, err)
		}
		defer rows.Close()
		out := []fiber.Map{}
		for rows.Next() {
			var id int64
			var email, name string
			if err := rows.Scan(&id, &email, &name); err != nil {
				return serverError(c, err)
			}
			out = append(out, fiber.Map{"lead_id": id, "email": email, "name": name})
		}
		return c.JSON(out)
	})

	app.Get("/sales-team", admin, func(c *fiber.Ctx) error {
		rows, err := pool.Query(c.Context(), `SELECT email, COALESCE(NULLIF(name,''), email) FROM users
			WHERE active AND role IN ('sales','admin') ORDER BY role DESC, email`)
		if err != nil {
			return serverError(c, err)
		}
		defer rows.Close()
		out := []fiber.Map{}
		for rows.Next() {
			var email, name string
			if err := rows.Scan(&email, &name); err != nil {
				return serverError(c, err)
			}
			out = append(out, fiber.Map{"email": email, "name": name})
		}
		return c.JSON(out)
	})

	app.Put("/leads/:id/assignment", staff, func(c *fiber.Ctx) error {
		id, err := strconv.ParseInt(c.Params("id"), 10, 64)
		if err != nil || id <= 0 {
			return c.Status(400).JSON(fiber.Map{"error": "invalid lead id"})
		}
		var in struct {
			Assignee string `json:"assignee"`
		}
		if err := c.BodyParser(&in); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		in.Assignee = strings.ToLower(strings.TrimSpace(in.Assignee))
		me := currentUser(c)
		isAdmin := me.Role == "admin"

		var current *string
		if err := pool.QueryRow(c.Context(), `SELECT assigned_to FROM leads WHERE id=$1 AND anonymized_at IS NULL`, id).Scan(&current); err != nil {
			return c.Status(404).JSON(fiber.Map{"error": "lead not found"})
		}
		cur := ""
		if current != nil {
			cur = *current
		}
		if !isAdmin {
			// sales: take an unowned lead for yourself, or release your own. Nothing else.
			takeOwn := in.Assignee == strings.ToLower(me.Email) && (cur == "" || cur == in.Assignee)
			releaseOwn := in.Assignee == "" && cur == strings.ToLower(me.Email)
			if !takeOwn && !releaseOwn {
				return c.Status(403).JSON(fiber.Map{"error": "เซลส์รับได้เฉพาะลีดที่ยังไม่มีเจ้าของ หรือปล่อยลีดของตัวเอง การมอบให้คนอื่นทำได้เฉพาะแอดมิน"})
			}
		}
		if in.Assignee == "" {
			if _, err := pool.Exec(c.Context(), `UPDATE leads SET assigned_to=NULL, assigned_at=NULL WHERE id=$1`, id); err != nil {
				return serverError(c, err)
			}
		} else {
			var ok bool
			if err := pool.QueryRow(c.Context(), `SELECT EXISTS (SELECT 1 FROM users WHERE lower(email)=$1 AND active AND role IN ('sales','admin'))`, in.Assignee).Scan(&ok); err != nil {
				return serverError(c, err)
			}
			if !ok {
				return c.Status(400).JSON(fiber.Map{"error": "ผู้รับผิดชอบต้องเป็นเซลส์หรือแอดมินที่ยังใช้งานอยู่"})
			}
			if _, err := pool.Exec(c.Context(), `UPDATE leads SET assigned_to=$1, assigned_at=now() WHERE id=$2`, in.Assignee, id); err != nil {
				return serverError(c, err)
			}
		}
		auditUser(c, pool, "lead_assigned", "lead:"+strconv.FormatInt(id, 10), in.Assignee)
		return c.JSON(fiber.Map{"ok": true, "assignee": in.Assignee})
	})
}
