package main

// Who may change what on a campaign.
//
//	creator   (the marketing user who made the campaign): edits everything in it - AI plan, content drafts, the product
//	admin     may edit any campaign; every edit of somebody else's campaign is written to the audit log
//	approver  (marketing or admin, a different person from the creator): sees everything, can only approve or reject.
//	          Rejecting needs a written reason, which the creator then sees on the content
//	sales     view only
//
// Editing content that was already approved sends it back to "pending approval" and clears the approval.
// Old campaigns made before this rule existed have no creator; any marketing user may still edit those.

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxReasonLen = 1000
const maxContentLen = 5000

// initRoles adds the columns for the rules above. Run it after initAuth and initAI.
func initRoles(ctx context.Context, pool *pgxpool.Pool) error {
	for _, s := range []string{
		`ALTER TABLE users     ADD COLUMN IF NOT EXISTS job_title TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS created_by TEXT`,
		`ALTER TABLE contents  ADD COLUMN IF NOT EXISTS reject_reason TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE contents  ADD COLUMN IF NOT EXISTS rejected_by TEXT`,
		`ALTER TABLE contents  ADD COLUMN IF NOT EXISTS rejected_at TIMESTAMPTZ`,
		`ALTER TABLE contents  ADD COLUMN IF NOT EXISTS edited_by TEXT`,
		`ALTER TABLE contents  ADD COLUMN IF NOT EXISTS edited_at TIMESTAMPTZ`,
		`ALTER TABLE contents  ADD COLUMN IF NOT EXISTS submitted_at TIMESTAMPTZ`,
		// contents sent before this column existed: the nearest time we know
		`UPDATE contents SET submitted_at = CASE WHEN status::text = 'pending_approval' THEN COALESCE(edited_at, created_at)
		   ELSE COALESCE(approved_at, rejected_at, edited_at, created_at) END
		 WHERE submitted_at IS NULL AND status::text <> 'draft'`,
	} {
		if _, err := pool.Exec(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

// ---- pure rules (unit tested) ----

func sameEmail(a *string, b string) bool { return a != nil && b != "" && strings.EqualFold(*a, b) }

// mayEditCampaign: allowed, and override=true when an admin edits somebody else's campaign (must be audited).
func mayEditCampaign(role, email string, creator *string) (allowed, override bool) {
	switch role {
	case "admin":
		return true, creator != nil && *creator != "" && !sameEmail(creator, email)
	case "marketing":
		if creator == nil || *creator == "" {
			return true, false // campaign from before creators were recorded
		}
		return sameEmail(creator, email), false
	}
	return false, false
}

// mayEditProduct: used = campaigns that use the product, mine = those made by this user (or with no creator).
// A product nobody uses yet can be edited by any writer (it is being set up); once campaigns use it, only their
// creators and admins may change it.
func mayEditProduct(role string, used, mine int) (allowed, override bool) {
	switch role {
	case "admin":
		return true, used > 0 && mine == 0
	case "marketing":
		return used == 0 || mine > 0, false
	}
	return false, false
}

// mayDecide: the approver (or rejecter) must be a different person from whoever created the content and the campaign.
func mayDecide(email string, contentCreator, campaignCreator *string) bool {
	return !sameEmail(contentCreator, email) && !sameEmail(campaignCreator, email)
}

// cleanReason trims the rejection reason; ok is false when it is empty or too long.
func cleanReason(s string) (string, bool) {
	s = strings.TrimSpace(s)
	return s, s != "" && len([]rune(s)) <= maxReasonLen
}

// ---- who is who ----

type person struct {
	Name  string `json:"name"`
	Title string `json:"title"`
	Role  string `json:"role"` // admin, marketing or sales
}

type people map[string]person // key: lower-case email

func loadPeople(ctx context.Context, pool *pgxpool.Pool) people {
	out := people{}
	rows, err := pool.Query(ctx, `SELECT lower(email), name, job_title, role::text FROM users`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var e string
		var p person
		if rows.Scan(&e, &p.Name, &p.Title, &p.Role) == nil {
			out[e] = p
		}
	}
	return out
}

func (p people) of(email *string) person {
	if email == nil {
		return person{}
	}
	return p[strings.ToLower(*email)]
}

func (p people) fill(x *Content) {
	c, a, r := p.of(x.CreatedBy), p.of(x.ApprovedBy), p.of(x.RejectedBy)
	x.CreatedByName, x.CreatedByTitle, x.CreatedByRole = c.Name, c.Title, c.Role
	x.ApprovedByName, x.ApprovedByTitle, x.ApprovedByRole = a.Name, a.Title, a.Role
	x.RejectedByName, x.RejectedByTitle, x.RejectedByRole = r.Name, r.Title, r.Role
}

// ---- middleware ----

func denyEdit(c *fiber.Ctx, what string) error {
	return c.Status(403).JSON(fiber.Map{"error": "only the creator of this " + what + " (or an admin) can change it"})
}

func authorizeCampaign(c *fiber.Ctx, pool *pgxpool.Pool, creator *string, target string) error {
	u := currentUser(c)
	ok, override := mayEditCampaign(u.Role, u.Email, creator)
	if !ok {
		return denyEdit(c, "campaign")
	}
	if override {
		auditUser(c, pool, "admin_edit_other_campaign", target, c.Method()+" "+c.Path())
	}
	return c.Next()
}

// campaignEditor guards routes with /campaigns/:id in the path.
func campaignEditor(pool *pgxpool.Pool) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id, err := c.ParamsInt("id")
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid campaign id"})
		}
		var creator *string
		err = pool.QueryRow(c.UserContext(), `SELECT created_by FROM campaigns WHERE id=$1`, id).Scan(&creator)
		if errors.Is(err, pgx.ErrNoRows) {
			return c.Status(404).JSON(fiber.Map{"error": "campaign not found"})
		}
		if err != nil {
			return serverError(c, err)
		}
		return authorizeCampaign(c, pool, creator, "campaign:"+strconv.Itoa(id))
	}
}

// contentEditor guards routes with /contents/:id in the path (the rule of the content's campaign applies).
func contentEditor(pool *pgxpool.Pool) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id, err := c.ParamsInt("id")
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid content id"})
		}
		var creator *string
		err = pool.QueryRow(c.UserContext(),
			`SELECT k.created_by FROM contents c JOIN campaigns k ON k.id=c.campaign_id WHERE c.id=$1`, id).Scan(&creator)
		if errors.Is(err, pgx.ErrNoRows) {
			return c.Status(404).JSON(fiber.Map{"error": "content not found"})
		}
		if err != nil {
			return serverError(c, err)
		}
		return authorizeCampaign(c, pool, creator, "content:"+strconv.Itoa(id))
	}
}

// productEditor guards routes with /products/:id in the path.
func productEditor(pool *pgxpool.Pool) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id, err := strconv.ParseInt(c.Params("id"), 10, 64)
		if err != nil || id <= 0 {
			return c.Status(400).JSON(fiber.Map{"error": "invalid product id"})
		}
		u := currentUser(c)
		var used, mine int
		err = pool.QueryRow(c.UserContext(),
			`SELECT count(*), count(*) FILTER (WHERE created_by IS NULL OR created_by='' OR lower(created_by)=lower($2))
			 FROM campaigns WHERE product_id=$1`, id, u.Email).Scan(&used, &mine)
		if err != nil {
			return serverError(c, err)
		}
		ok, override := mayEditProduct(u.Role, used, mine)
		if !ok {
			return denyEdit(c, "product")
		}
		if override {
			auditUser(c, pool, "admin_edit_other_product", "product:"+strconv.FormatInt(id, 10), c.Method()+" "+c.Path())
		}
		return c.Next()
	}
}

// ---- content routes that implement the rules ----

func registerContentRuleRoutes(app fiber.Router, pool *pgxpool.Pool) {
	writers := requireRole("marketing", "admin")

	// Edit the text of a draft. Approved text goes back to "pending approval" (the approval no longer covers it).
	app.Put("/contents/:id", writers, contentEditor(pool), func(c *fiber.Ctx) error {
		id, _ := c.ParamsInt("id")
		var in struct {
			Body string `json:"body"`
		}
		if err := c.BodyParser(&in); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		body := strings.TrimSpace(in.Body)
		if body == "" || len([]rune(body)) > maxContentLen {
			return c.Status(400).JSON(fiber.Map{"error": "body must be 1-5000 characters"})
		}
		var status string
		err := pool.QueryRow(c.UserContext(),
			`UPDATE contents SET body=$1, edited_by=$2, edited_at=now(),
			   status      = CASE WHEN status IN ('approved','published') THEN 'pending_approval'::content_status ELSE status END,
			   submitted_at = CASE WHEN status IN ('approved','published') THEN now() ELSE submitted_at END,
			   approved_by = CASE WHEN status IN ('approved','published') THEN NULL ELSE approved_by END,
			   approved_at = CASE WHEN status IN ('approved','published') THEN NULL ELSE approved_at END
			 WHERE id=$3 AND body IS DISTINCT FROM $1 RETURNING status::text`, body, currentUser(c).Email, id).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) { // unchanged text (the guard already proved the content exists)
			_ = pool.QueryRow(c.UserContext(), `SELECT status::text FROM contents WHERE id=$1`, id).Scan(&status)
			return c.JSON(fiber.Map{"status": status, "changed": false})
		}
		if err != nil {
			return serverError(c, err)
		}
		auditUser(c, pool, "content_edited", "content:"+strconv.Itoa(id), "status now "+status)
		return c.JSON(fiber.Map{"status": status, "changed": true})
	})

	// The approver rejects with a reason; the creator sees it and can edit and resubmit.
	app.Post("/contents/:id/reject", writers, func(c *fiber.Ctx) error {
		id, err := c.ParamsInt("id")
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid content id"})
		}
		var in struct {
			Reason string `json:"reason"`
		}
		_ = c.BodyParser(&in)
		reason, ok := cleanReason(in.Reason)
		if !ok {
			return c.Status(400).JSON(fiber.Map{"error": "a reason (1-1000 characters) is required to reject"})
		}
		var cc, kc *string
		err = pool.QueryRow(c.UserContext(),
			`SELECT c.created_by, k.created_by FROM contents c JOIN campaigns k ON k.id=c.campaign_id WHERE c.id=$1`, id).Scan(&cc, &kc)
		if errors.Is(err, pgx.ErrNoRows) {
			return c.Status(409).JSON(fiber.Map{"error": "content not found or not pending approval"})
		}
		if err != nil {
			return serverError(c, err)
		}
		email := currentUser(c).Email
		if !mayDecide(email, cc, kc) {
			auditUser(c, pool, "content_reject_denied", "content:"+strconv.Itoa(id), "rejecter is the creator")
			return c.Status(403).JSON(fiber.Map{"error": "approver must be a different person from the creator"})
		}
		tag, err := pool.Exec(c.UserContext(),
			`UPDATE contents SET status='rejected', reject_reason=$1, rejected_by=$2, rejected_at=now()
			 WHERE id=$3 AND status='pending_approval'`, reason, email, id)
		if err != nil {
			return serverError(c, err)
		}
		if tag.RowsAffected() == 0 {
			return c.Status(409).JSON(fiber.Map{"error": "content not found or not pending approval"})
		}
		auditUser(c, pool, "content_rejected", "content:"+strconv.Itoa(id), trunc(reason, 300))
		return c.JSON(fiber.Map{"status": "rejected"})
	})
}
