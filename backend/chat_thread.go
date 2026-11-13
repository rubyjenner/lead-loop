package main

// Two-way conversation between a customer and the people handling the lead.
//
// "Staff mode": once a lead is handed to people (status qualified or handed_off, or a salesperson has already
// written to the customer) and until the sale is closed or lost, the bot stays quiet. What the customer types
// is kept in customer_messages so sales/admin can read it on the lead page, and sales answer from there
// (POST /leads/:id/reply). Before staff mode nothing but the detected event types is kept, as before.
//
// PDPA: the consent notice tells the customer this (see ChatConsentPrompt). Stored texts are deleted by a database
// trigger as soon as the customer withdraws consent or the lead is anonymized (see initStaffReplies), and they are
// never sent to an AI provider.

import (
	"context"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

const customerMessageMaxRunes = 500

// staffMode is evaluated BEFORE the current message changes the score, so the message that triggers the
// hand-off still gets the normal bot answer ("ส่งเรื่องให้ทีมงานแล้ว") and every later one goes to people.
func (ch *chatChannelAPI) staffMode(ctx context.Context, leadID int64) bool {
	var on bool
	err := ch.leads.pool.QueryRow(ctx, `SELECT CASE
			WHEN status IN ('closed','lost') THEN FALSE
			WHEN status IN ('qualified','handed_off') THEN TRUE
			ELSE EXISTS (SELECT 1 FROM staff_replies WHERE lead_id=$1)
		END FROM leads WHERE id=$1`, leadID).Scan(&on)
	if err != nil {
		log.Printf("staffMode lead %d: %v", leadID, err)
		return false // on error keep the bot answering rather than leaving the customer without a reply
	}
	return on
}

func (ch *chatChannelAPI) saveCustomerMessage(ctx context.Context, leadID int64, text string) error {
	body := strings.TrimSpace(text)
	if body == "" {
		return nil
	}
	if r := []rune(body); len(r) > customerMessageMaxRunes {
		body = string(r[:customerMessageMaxRunes])
	}
	_, err := ch.leads.pool.Exec(ctx, `INSERT INTO customer_messages (lead_id, body) VALUES ($1,$2)`, leadID, body)
	if err != nil {
		return err
	}
	var owner, name string
	_ = ch.leads.pool.QueryRow(ctx, `SELECT COALESCE(assigned_to,''),display_name FROM leads WHERE id=$1`, leadID).Scan(&owner, &name)
	if owner != "" {
		notifyUser(ctx, ch.leads.pool, owner, "lead_message", "มีข้อความใหม่จากลูกค้า", name, "/leads/"+strconv.FormatInt(leadID, 10), "lead:"+strconv.FormatInt(leadID, 10)+":message")
	} else {
		notifyRoles(ctx, ch.leads.pool, []string{"sales", "admin"}, "lead_message", "มีข้อความใหม่จากลูกค้า", name, "/leads/"+strconv.FormatInt(leadID, 10), "lead:"+strconv.FormatInt(leadID, 10)+":message")
	}
	return nil
}

type threadRow struct {
	ID        int64     `json:"id"`
	Dir       string    `json:"dir"` // "in" = from the customer, "out" = from staff
	Body      string    `json:"body"`
	SentBy    string    `json:"sent_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// GET /leads/:id/thread -> the conversation (customer and staff messages, oldest first).
// Opening it marks the customer's messages as read (they stop counting in the inbox).
func registerThreadRoute(app fiber.Router, pool *pgxpool.Pool) {
	app.Get("/leads/:id/thread", func(c *fiber.Ctx) error {
		id, err := leadID(c)
		if err != nil {
			return leadErr(c, 400, "invalid lead id")
		}
		rows, err := pool.Query(c.Context(), `
			SELECT id, dir, body, sent_by, created_at FROM (
				SELECT id, 'in' AS dir, body, ''::text AS sent_by, created_at FROM customer_messages WHERE lead_id=$1
				UNION ALL
				SELECT id, 'out', body, sent_by, created_at FROM staff_replies WHERE lead_id=$1
			) t ORDER BY created_at, dir LIMIT 200`, id)
		if err != nil {
			return serverError(c, err)
		}
		defer rows.Close()
		list := []threadRow{}
		for rows.Next() {
			var r threadRow
			if err := rows.Scan(&r.ID, &r.Dir, &r.Body, &r.SentBy, &r.CreatedAt); err != nil {
				return serverError(c, err)
			}
			list = append(list, r)
		}
		if err := rows.Err(); err != nil {
			return serverError(c, err)
		}
		if _, err := pool.Exec(c.Context(), `UPDATE customer_messages SET seen_at=now() WHERE lead_id=$1 AND seen_at IS NULL`, id); err != nil {
			log.Printf("thread mark seen lead %d: %v", id, err)
		}
		return c.JSON(list)
	})
}
