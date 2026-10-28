package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Guardrail: the AI may only use product/brand facts we give it.
const systemPrompt = `คุณเป็นผู้ช่วยการตลาดและการขาย ตอบเป็นภาษาไทย
กฎ: ใช้เฉพาะข้อมูลสินค้าและแบรนด์ที่ให้มาเท่านั้น ห้ามแต่งราคา โปรโมชัน หรือคุณสมบัติที่ไม่มีในข้อมูล
ห้ามโฆษณาเกินจริง ถ้าข้อมูลไม่พอ ให้เขียนจากข้อมูลที่มีแบบทั่วไปโดยไม่แต่งข้อเท็จจริงเพิ่ม และห้ามถามกลับหรือขอข้อมูลเพิ่มแทนการเขียนงาน`

// initAI adds the columns that record what produced each AI result (generated_by) and the ai_usage table.
func initAI(ctx context.Context, pool *pgxpool.Pool) error {
	if err := initUsage(ctx, pool); err != nil {
		return err
	}
	for _, s := range []string{
		`ALTER TABLE contents  ADD COLUMN IF NOT EXISTS generated_by TEXT NOT NULL DEFAULT 'human'`,
		`ALTER TABLE contents  ADD COLUMN IF NOT EXISTS created_by TEXT`,
		`ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS plan_generated_by TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := pool.Exec(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

// loadCampaign loads a campaign together with its product (if any).
func loadCampaign(ctx context.Context, pool *pgxpool.Pool, id int) (campaignInfo, error) {
	var (
		ci                   campaignInfo
		pname, pdesc, pbrand *string
	)
	err := pool.QueryRow(ctx,
		`SELECT c.name, c.goal, c.target_audience, c.budget::float8,
		        p.name, p.description, p.price::float8, p.brand_info
		 FROM campaigns c LEFT JOIN products p ON p.id = c.product_id
		 WHERE c.id = $1`, id,
	).Scan(&ci.Name, &ci.Goal, &ci.Audience, &ci.Budget, &pname, &pdesc, &ci.Price, &pbrand)
	if err != nil {
		return ci, err
	}
	if pname != nil {
		ci.Product = *pname
	}
	if pdesc != nil {
		ci.ProductDesc = *pdesc
	}
	if pbrand != nil {
		ci.Brand = *pbrand
	}
	return ci, nil
}

func registerAIRoutes(app *fiber.App, pool *pgxpool.Pool, q JobQueue) {
	writers := requireRole("marketing", "admin")

	// over reports whether today's AI quota is used up. If the check itself fails we also fall back to the
	// template, so a database problem can never cause unbounded AI spending.
	over := func(c *fiber.Ctx, email string) bool {
		o, err := quotaExceeded(c.UserContext(), pool, email)
		if err != nil {
			log.Printf("quota check failed: %v", err)
		}
		return o
	}
	// enqueue schedules an AI retry when providers were tried and all failed (not for quota or missing keys).
	enqueue := func(c *fiber.Ctx, r Result, j Job) bool {
		if q == nil || r.By != "template" || r.Reason != "unavailable" {
			return false
		}
		if err := q.Push(c.UserContext(), j, time.Now().Add(jobBackoff(0))); err != nil {
			log.Printf("cannot queue ai retry: %v", err)
			return false
		}
		return true
	}

	// Step 2: plan the campaign (Claude -> Gemini -> template). Saved to campaigns.plan,
	// status draft -> planned. generated_by says what produced it.
	app.Post("/campaigns/:id/plan", writers, func(c *fiber.Ctx) error {
		id, err := c.ParamsInt("id")
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid campaign id"})
		}
		ci, err := loadCampaign(c.UserContext(), pool, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return c.Status(404).JSON(fiber.Map{"error": "campaign not found"})
		}
		if err != nil {
			return serverError(c, err)
		}
		email := currentUser(c).Email
		r := generate(c.UserContext(), systemPrompt, planPrompt(ci), !over(c, email),
			func() string { return templatePlan(ci) })
		recordUsage(c.UserContext(), pool, email, id, "plan", r)
		if _, err := pool.Exec(c.UserContext(),
			`UPDATE campaigns SET plan=$1, plan_generated_by=$2,
			   status = CASE WHEN status='draft' THEN 'planned'::campaign_status ELSE status END
			 WHERE id=$3`, r.Text, r.By, id); err != nil {
			return serverError(c, err)
		}
		queued := enqueue(c, r, Job{Kind: "plan", CampaignID: id, UserEmail: email})
		auditUser(c, pool, "campaign_planned", fmt.Sprintf("campaign:%d", id), "generated_by="+r.By)
		return c.JSON(fiber.Map{"campaign_id": id, "plan": r.Text, "generated_by": r.By,
			"fallback_reason": r.Reason, "retry_queued": queued})
	})

	// Step 3: draft content variants; saved as 'draft' (a human must still submit + approve)
	app.Post("/campaigns/:id/generate-contents", writers, func(c *fiber.Ctx) error {
		id, err := c.ParamsInt("id")
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid campaign id"})
		}
		in := struct {
			Channel string `json:"channel"`
			Count   int    `json:"count"`
		}{Channel: "line", Count: 3}
		if len(c.Body()) > 0 {
			if err := c.BodyParser(&in); err != nil {
				return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
			}
		}
		if in.Channel == "" {
			in.Channel = "line"
		}
		if in.Count < 1 || in.Count > 5 {
			return c.Status(400).JSON(fiber.Map{"error": "count must be between 1 and 5"})
		}

		ci, err := loadCampaign(c.UserContext(), pool, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return c.Status(404).JSON(fiber.Map{"error": "campaign not found"})
		}
		if err != nil {
			return serverError(c, err)
		}
		email := currentUser(c).Email
		r := generate(c.UserContext(), systemPrompt, contentsPrompt(ci, in.Channel, in.Count), !over(c, email),
			func() string { return strings.Join(templateContents(ci, in.Channel, in.Count), "\n---\n") })
		recordUsage(c.UserContext(), pool, email, id, "generate_contents", r)

		out := []Content{}
		ids := []int64{}
		for _, part := range splitDrafts(r.Text, in.Count) {
			x, err := scanContent(pool.QueryRow(c.UserContext(),
				`INSERT INTO contents (campaign_id, channel, body, generated_by, created_by) VALUES ($1,$2,$3,$4,$5)
				 RETURNING `+contentCols, id, in.Channel, part, r.By, email))
			if err != nil {
				return serverError(c, err)
			}
			out = append(out, x)
			ids = append(ids, x.ID)
		}
		queued := enqueue(c, r, Job{Kind: "contents", CampaignID: id, ContentIDs: ids, Channel: in.Channel, UserEmail: email})
		auditUser(c, pool, "contents_generated", fmt.Sprintf("campaign:%d", id),
			fmt.Sprintf("generated_by=%s count=%d", r.By, len(out)))
		return c.Status(201).JSON(fiber.Map{"generated_by": r.By, "fallback_reason": r.Reason,
			"retry_queued": queued, "contents": out})
	})

	// How much of today's AI quota the logged-in user has used.
	app.Get("/ai-usage/me", func(c *fiber.Ctx) error {
		n, err := usageToday(c.UserContext(), pool, currentUser(c).Email)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(fiber.Map{
			"used_today":           n,
			"per_user_daily_limit": envInt("AI_DAILY_QUOTA_PER_USER", 30),
			"token_cap_daily":      envInt("AI_DAILY_TOKEN_CAP", 0),
		})
	})

	// Admin: requests and tokens per day and provider (default last 7 days, max 90), for cost estimates.
	app.Get("/ai-usage", requireRole("admin"), func(c *fiber.Ctx) error {
		days := c.QueryInt("days", 7)
		if days < 1 {
			days = 1
		}
		if days > 90 {
			days = 90
		}
		rows, err := pool.Query(c.UserContext(),
			`SELECT (at AT TIME ZONE 'Asia/Bangkok')::date::text, provider, count(*),
			        coalesce(sum(input_tokens),0), coalesce(sum(output_tokens),0)
			 FROM ai_usage WHERE at >= now() - make_interval(days => $1::int)
			 GROUP BY 1, 2 ORDER BY 1 DESC, 2`, days)
		if err != nil {
			return serverError(c, err)
		}
		defer rows.Close()
		type row struct {
			Day          string `json:"day"`
			Provider     string `json:"provider"`
			Requests     int    `json:"requests"`
			InputTokens  int64  `json:"input_tokens"`
			OutputTokens int64  `json:"output_tokens"`
		}
		out := []row{}
		for rows.Next() {
			var x row
			if err := rows.Scan(&x.Day, &x.Provider, &x.Requests, &x.InputTokens, &x.OutputTokens); err != nil {
				return serverError(c, err)
			}
			out = append(out, x)
		}
		return c.JSON(fiber.Map{
			"days":                 days,
			"per_user_daily_limit": envInt("AI_DAILY_QUOTA_PER_USER", 30),
			"token_cap_daily":      envInt("AI_DAILY_TOKEN_CAP", 0),
			"usage":                out,
		})
	})
}
