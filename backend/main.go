package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Campaign struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	Goal           string    `json:"goal"`
	TargetAudience string    `json:"target_audience"`
	Budget         float64   `json:"budget"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
}

type Content struct {
	ID         int64      `json:"id"`
	CampaignID int64      `json:"campaign_id"`
	Channel    string     `json:"channel"`
	Body       string     `json:"body"`
	Status     string     `json:"status"`
	ApprovedBy *string    `json:"approved_by"`
	ApprovedAt *time.Time `json:"approved_at"`
	PublishAt  *time.Time `json:"publish_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

const contentCols = `id, campaign_id, channel, body, status::text, approved_by, approved_at, publish_at, created_at`

func scanContent(row interface{ Scan(...any) error }) (Content, error) {
	var x Content
	err := row.Scan(&x.ID, &x.CampaignID, &x.Channel, &x.Body, &x.Status,
		&x.ApprovedBy, &x.ApprovedAt, &x.PublishAt, &x.CreatedAt)
	return x, err
}

func main() {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	app := fiber.New()

	// Declare UTF-8 on JSON responses so clients like Windows PowerShell 5.1 decode Thai correctly
	app.Use(func(c *fiber.Ctx) error {
		err := c.Next()
		if string(c.Response().Header.ContentType()) == fiber.MIMEApplicationJSON {
			c.Response().Header.SetContentType(fiber.MIMEApplicationJSONCharsetUTF8)
		}
		return err
	})

	app.Get("/health", func(c *fiber.Ctx) error {
		if err := pool.Ping(c.Context()); err != nil {
			return c.Status(503).JSON(fiber.Map{"status": "db down"})
		}
		return c.JSON(fiber.Map{"status": "ok"})
	})

	// Step 1: team creates a campaign from product info + goal + budget
	app.Post("/campaigns", func(c *fiber.Ctx) error {
		var in Campaign
		if err := c.BodyParser(&in); err != nil || in.Name == "" {
			return c.Status(400).JSON(fiber.Map{"error": "name is required"})
		}
		err := pool.QueryRow(c.Context(),
			`INSERT INTO campaigns (name, goal, target_audience, budget)
			 VALUES ($1,$2,$3,$4) RETURNING id, status, created_at`,
			in.Name, in.Goal, in.TargetAudience, in.Budget,
		).Scan(&in.ID, &in.Status, &in.CreatedAt)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(201).JSON(in)
	})

	app.Get("/campaigns", func(c *fiber.Ctx) error {
		rows, err := pool.Query(c.Context(),
			`SELECT id, name, goal, target_audience, budget::float8, status::text, created_at
			 FROM campaigns ORDER BY id DESC`)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		defer rows.Close()
		out := []Campaign{}
		for rows.Next() {
			var x Campaign
			if err := rows.Scan(&x.ID, &x.Name, &x.Goal, &x.TargetAudience, &x.Budget, &x.Status, &x.CreatedAt); err != nil {
				return c.Status(500).JSON(fiber.Map{"error": err.Error()})
			}
			out = append(out, x)
		}
		return c.JSON(out)
	})

	// Step 3: AI (or a person, for now) creates a content draft for a campaign
	app.Post("/campaigns/:id/contents", func(c *fiber.Ctx) error {
		campaignID, err := c.ParamsInt("id")
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid campaign id"})
		}
		var in struct {
			Channel string `json:"channel"`
			Body    string `json:"body"`
		}
		if err := c.BodyParser(&in); err != nil || in.Channel == "" || in.Body == "" {
			return c.Status(400).JSON(fiber.Map{"error": "channel and body are required"})
		}
		var exists bool
		if err := pool.QueryRow(c.Context(),
			`SELECT EXISTS (SELECT 1 FROM campaigns WHERE id=$1)`, campaignID).Scan(&exists); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		if !exists {
			return c.Status(404).JSON(fiber.Map{"error": "campaign not found"})
		}
		out, err := scanContent(pool.QueryRow(c.Context(),
			`INSERT INTO contents (campaign_id, channel, body) VALUES ($1,$2,$3)
			 RETURNING `+contentCols, campaignID, in.Channel, in.Body))
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(201).JSON(out)
	})

	app.Get("/campaigns/:id/contents", func(c *fiber.Ctx) error {
		campaignID, err := c.ParamsInt("id")
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid campaign id"})
		}
		rows, err := pool.Query(c.Context(),
			`SELECT `+contentCols+` FROM contents WHERE campaign_id=$1 ORDER BY id`, campaignID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		defer rows.Close()
		out := []Content{}
		for rows.Next() {
			x, err := scanContent(rows)
			if err != nil {
				return c.Status(500).JSON(fiber.Map{"error": err.Error()})
			}
			out = append(out, x)
		}
		return c.JSON(out)
	})

	// draft or rejected -> pending_approval
	app.Post("/contents/:id/submit", func(c *fiber.Ctx) error {
		return setContentStatus(c, pool,
			`UPDATE contents SET status='pending_approval'
			 WHERE id=$1 AND status IN ('draft','rejected')`,
			"content not found or not in draft/rejected status", "pending_approval")
	})

	// Step 4: a human approves content before it can be published
	app.Post("/contents/:id/approve", func(c *fiber.Ctx) error {
		var in struct {
			ApprovedBy string `json:"approved_by"`
		}
		if err := c.BodyParser(&in); err != nil || in.ApprovedBy == "" {
			return c.Status(400).JSON(fiber.Map{"error": "approved_by is required"})
		}
		id, err := c.ParamsInt("id")
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid content id"})
		}
		tag, err := pool.Exec(c.Context(),
			`UPDATE contents SET status='approved', approved_by=$1, approved_at=now()
			 WHERE id=$2 AND status='pending_approval'`, in.ApprovedBy, id)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		if tag.RowsAffected() == 0 {
			return c.Status(409).JSON(fiber.Map{"error": "content not found or not pending approval"})
		}
		return c.JSON(fiber.Map{"status": "approved"})
	})

	app.Post("/contents/:id/reject", func(c *fiber.Ctx) error {
		return setContentStatus(c, pool,
			`UPDATE contents SET status='rejected'
			 WHERE id=$1 AND status='pending_approval'`,
			"content not found or not pending approval", "rejected")
	})

	log.Fatal(app.Listen(":8080"))
}

func setContentStatus(c *fiber.Ctx, pool *pgxpool.Pool, query, conflictMsg, status string) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid content id"})
	}
	tag, err := pool.Exec(c.Context(), query, id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	if tag.RowsAffected() == 0 {
		return c.Status(409).JSON(fiber.Map{"error": conflictMsg})
	}
	return c.JSON(fiber.Map{"status": status})
}
