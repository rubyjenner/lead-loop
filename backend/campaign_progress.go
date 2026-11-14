package main

// Where is each campaign in the loop? Pure rules (campaignSteps / campaignNote) plus two read-only routes:
//
//	GET /campaign-progress       every campaign: {"<id>": {steps, current, note}}   (any signed-in role)
//	GET /campaign-progress/:id   one campaign
//
// Steps: created -> planned -> drafted -> submitted -> approved -> leads -> handed off -> closed.
// Counts only; no personal data. Anonymized leads are not counted.

import (
	"fmt"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

type progressCounts struct {
	HasPlan                                       bool
	Draft, Pending, Approved, Published, Rejected int
	Leads, Handed, Closed                         int
}

type progressStep struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Done   bool   `json:"done"`
	Detail string `json:"detail,omitempty"`
	Warn   bool   `json:"warn,omitempty"` // something here needs fixing (e.g. content was rejected)
}

func (p progressCounts) contents() int {
	return p.Draft + p.Pending + p.Approved + p.Published + p.Rejected
}

// campaignSteps lists the eight steps in order with whether each is reached and a short count.
func campaignSteps(p progressCounts) []progressStep {
	total := p.contents()
	okc := p.Approved + p.Published
	steps := []progressStep{
		{Key: "created", Label: "สร้างแคมเปญ", Done: true},
		{Key: "planned", Label: "วางแผน", Done: p.HasPlan},
		{Key: "drafted", Label: "ร่างคอนเทนต์", Done: total > 0},
		{Key: "submitted", Label: "ส่งอนุมัติ", Done: p.Pending+okc+p.Rejected > 0},
		// "approved" means everything that was submitted is approved: not while something is waiting or was rejected
		{Key: "approved", Label: "อนุมัติแล้ว", Done: okc > 0 && p.Pending == 0 && p.Rejected == 0},
		{Key: "leads", Label: "ลีดเข้ามา", Done: p.Leads > 0},
		{Key: "handed", Label: "ส่งต่อเซลส์", Done: p.Handed > 0},
		{Key: "closed", Label: "ปิดการขาย", Done: p.Closed > 0},
	}
	if total > 0 {
		steps[2].Detail = fmt.Sprintf("%d ชิ้น", total)
		steps[4].Detail = fmt.Sprintf("%d/%d", okc, total)
		if p.Rejected > 0 {
			steps[4].Warn = true
			steps[4].Detail += fmt.Sprintf(" · ปฏิเสธ %d", p.Rejected)
		}
	}
	if p.Leads > 0 {
		steps[5].Detail = fmt.Sprintf("%d ราย", p.Leads)
		steps[6].Detail = fmt.Sprintf("%d ราย", p.Handed)
		steps[7].Detail = fmt.Sprintf("%d ราย", p.Closed)
	}
	return steps
}

// campaignCurrent is the index of the furthest step reached (a later step can be reached without an earlier one,
// e.g. leads arriving from LINE before any content was approved; the furthest one is what people want to see).
func campaignCurrent(steps []progressStep) int {
	cur := 0
	for i, s := range steps {
		if s.Done {
			cur = i
		}
	}
	return cur
}

// campaignNote is the one thing that needs a person right now, or "" when nothing does.
func campaignNote(p progressCounts) string {
	switch {
	case p.Pending > 0:
		return fmt.Sprintf("รออนุมัติ %d ชิ้น", p.Pending)
	case p.Rejected > 0:
		return fmt.Sprintf("ถูกปฏิเสธ %d ชิ้น รอผู้สร้างแก้", p.Rejected)
	case !p.HasPlan:
		return "ยังไม่มีแผน"
	case p.contents() == 0:
		return "ยังไม่มีคอนเทนต์"
	case p.Draft > 0 && p.Approved+p.Published == 0:
		return fmt.Sprintf("ร่าง %d ชิ้น ยังไม่ส่งอนุมัติ", p.Draft)
	}
	return ""
}

func progressJSON(p progressCounts) fiber.Map {
	steps := campaignSteps(p)
	return fiber.Map{"steps": steps, "current": campaignCurrent(steps), "note": campaignNote(p)}
}

const progressSQL = `
SELECT c.id,
  (btrim(COALESCE(c.plan,'')) <> ''),
  (SELECT count(*) FROM contents x WHERE x.campaign_id=c.id AND x.status='draft'),
  (SELECT count(*) FROM contents x WHERE x.campaign_id=c.id AND x.status='pending_approval'),
  (SELECT count(*) FROM contents x WHERE x.campaign_id=c.id AND x.status='approved'),
  (SELECT count(*) FROM contents x WHERE x.campaign_id=c.id AND x.status='published'),
  (SELECT count(*) FROM contents x WHERE x.campaign_id=c.id AND x.status='rejected'),
  (SELECT count(*) FROM leads l WHERE l.campaign_id=c.id AND l.anonymized_at IS NULL),
  (SELECT count(*) FROM leads l WHERE l.campaign_id=c.id AND l.anonymized_at IS NULL AND l.status IN ('qualified','handed_off','won','closed')),
  (SELECT count(*) FROM leads l WHERE l.campaign_id=c.id AND l.anonymized_at IS NULL AND l.status IN ('won','closed'))
FROM campaigns c`

func registerCampaignProgress(app fiber.Router, pool *pgxpool.Pool) {
	load := func(c *fiber.Ctx, where string, args ...any) (map[string]fiber.Map, error) {
		rows, err := pool.Query(c.UserContext(), progressSQL+where, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := map[string]fiber.Map{}
		for rows.Next() {
			var id int64
			var p progressCounts
			if err := rows.Scan(&id, &p.HasPlan, &p.Draft, &p.Pending, &p.Approved, &p.Published, &p.Rejected, &p.Leads, &p.Handed, &p.Closed); err != nil {
				return nil, err
			}
			out[strconv.FormatInt(id, 10)] = progressJSON(p)
		}
		return out, rows.Err()
	}
	app.Get("/campaign-progress", func(c *fiber.Ctx) error {
		out, err := load(c, "")
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(out)
	})
	app.Get("/campaign-progress/:id", func(c *fiber.Ctx) error {
		id, err := c.ParamsInt("id")
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid campaign id"})
		}
		out, err := load(c, " WHERE c.id=$1", id)
		if err != nil {
			return serverError(c, err)
		}
		one, ok := out[strconv.Itoa(id)]
		if !ok {
			return c.Status(404).JSON(fiber.Map{"error": "campaign not found"})
		}
		return c.JSON(one)
	})
}
