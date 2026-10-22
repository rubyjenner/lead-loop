package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Guardrail: the AI may only use product/brand facts we give it.
const systemPrompt = `คุณเป็นผู้ช่วยการตลาดและการขาย ตอบเป็นภาษาไทย
กฎ: ใช้เฉพาะข้อมูลสินค้าและแบรนด์ที่ให้มาเท่านั้น ห้ามแต่งราคา โปรโมชัน หรือคุณสมบัติที่ไม่มีในข้อมูล
ห้ามโฆษณาเกินจริง ถ้าข้อมูลไม่พอให้บอกว่าต้องการข้อมูลเพิ่มอะไร`

// initAI adds the columns that record what produced each AI result (generated_by).
func initAI(ctx context.Context, pool *pgxpool.Pool) error {
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

func registerAIRoutes(app *fiber.App, pool *pgxpool.Pool) {
	writers := requireRole("marketing", "admin")

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
		plan, by := generate(c.UserContext(), systemPrompt,
			"วางแผนแคมเปญสั้นๆ ประกอบด้วย กลุ่มลูกค้าย่อย ช่องทาง ข้อความหลัก และตัวชี้วัดที่ใช้วัดผล\n\n"+ci.facts(),
			func() string { return templatePlan(ci) })
		if _, err := pool.Exec(c.UserContext(),
			`UPDATE campaigns SET plan=$1, plan_generated_by=$2,
			   status = CASE WHEN status='draft' THEN 'planned'::campaign_status ELSE status END
			 WHERE id=$3`, plan, by, id); err != nil {
			return serverError(c, err)
		}
		auditUser(c, pool, "campaign_planned", fmt.Sprintf("campaign:%d", id), "generated_by="+by)
		return c.JSON(fiber.Map{"campaign_id": id, "plan": plan, "generated_by": by})
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

		text, by := generate(c.UserContext(), systemPrompt,
			fmt.Sprintf("เขียนข้อความโฆษณา %d แบบ สำหรับช่องทาง %s แต่ละแบบคั่นด้วยบรรทัดที่มีเฉพาะ --- ไม่ต้องมีหัวข้อหรือเลขข้อ\n\n%s",
				in.Count, in.Channel, ci.facts()),
			func() string { return strings.Join(templateContents(ci, in.Channel, in.Count), "\n---\n") })

		out := []Content{}
		for _, part := range strings.Split(text, "\n---\n") {
			part = strings.Trim(strings.TrimSpace(part), "-")
			part = strings.TrimSpace(part)
			if part == "" || len(out) >= in.Count {
				continue
			}
			x, err := scanContent(pool.QueryRow(c.UserContext(),
				`INSERT INTO contents (campaign_id, channel, body, generated_by, created_by) VALUES ($1,$2,$3,$4,$5)
				 RETURNING `+contentCols, id, in.Channel, part, by, currentUser(c).Email))
			if err != nil {
				return serverError(c, err)
			}
			out = append(out, x)
		}
		auditUser(c, pool, "contents_generated", fmt.Sprintf("campaign:%d", id),
			fmt.Sprintf("generated_by=%s count=%d", by, len(out)))
		return c.Status(201).JSON(fiber.Map{"generated_by": by, "contents": out})
	})
}
