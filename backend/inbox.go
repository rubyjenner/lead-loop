package main

// GET /inbox: "what needs me now", shown as badges in the menu and a box on the overview page.
// Wire it up in main.go after registerDailyRoutes:
//
//	registerInboxRoutes(app, pool)
//
// It only counts, so it is cheap and holds no personal data. What each role sees:
//   - sales: leads that reached the hand-off threshold and nobody has taken yet (and the oldest wait)
//   - marketing/admin: content waiting for approval that the user did NOT create (they cannot approve their
//     own), and follow-up drafts waiting for approval; admin also sees waiting leads

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

func registerInboxRoutes(app fiber.Router, pool *pgxpool.Pool) {
	app.Get("/inbox", func(c *fiber.Ctx) error {
		u := currentUser(c)
		if u == nil {
			return c.Status(401).JSON(fiber.Map{"error": "unauthorized"})
		}
		ctx := c.Context()
		out := fiber.Map{}

		if u.Role == "sales" || u.Role == "admin" {
			var n int
			var oldest *time.Time
			// sales see leads that are theirs or nobody's; admin sees everything
			if err := pool.QueryRow(ctx, `SELECT count(*), min(updated_at) FROM leads
				WHERE status='qualified' AND anonymized_at IS NULL
				  AND ($1='admin' OR assigned_to IS NULL OR lower(assigned_to)=lower($2))`, u.Role, u.Email).Scan(&n, &oldest); err != nil {
				return serverError(c, err)
			}
			out["leads_ready"] = n
			out["leads_ready_oldest"] = oldest
			var unread int // leads where the customer wrote and nobody has opened the conversation yet
			if err := pool.QueryRow(ctx, `SELECT count(DISTINCT m.lead_id) FROM customer_messages m JOIN leads l ON l.id=m.lead_id
				WHERE m.seen_at IS NULL AND l.anonymized_at IS NULL
				  AND ($1='admin' OR l.assigned_to IS NULL OR lower(l.assigned_to)=lower($2))`, u.Role, u.Email).Scan(&unread); err != nil {
				return serverError(c, err)
			}
			out["messages_unread"] = unread
			var mine, unassigned int // open leads that belong to me / that nobody owns yet
			if err := pool.QueryRow(ctx, `SELECT
				count(*) FILTER (WHERE lower(assigned_to)=lower($1)),
				count(*) FILTER (WHERE assigned_to IS NULL)
				FROM leads WHERE status IN ('qualified','handed_off') AND anonymized_at IS NULL`, u.Email).Scan(&mine, &unassigned); err != nil {
				return serverError(c, err)
			}
			out["my_leads"] = mine
			if u.Role == "admin" {
				out["leads_unassigned"] = unassigned
			}
		}
		if u.Role == "marketing" || u.Role == "admin" {
			var contents, followups int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM contents
				WHERE status='pending_approval' AND COALESCE(created_by,'') <> $1`, u.Email).Scan(&contents); err != nil {
				return serverError(c, err)
			}
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM lead_followups f JOIN leads l ON l.id=f.lead_id
				WHERE f.status='pending_approval' AND l.anonymized_at IS NULL`).Scan(&followups); err != nil {
				return serverError(c, err)
			}
			out["contents_to_approve"] = contents
			out["followups_to_approve"] = followups
		}
		return c.JSON(out)
	})
}
