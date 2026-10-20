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

func main() {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	app := fiber.New()

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

	// Step 4: a human approves content before it can be published
	app.Post("/contents/:id/approve", func(c *fiber.Ctx) error {
		var in struct {
			ApprovedBy string `json:"approved_by"`
		}
		if err := c.BodyParser(&in); err != nil || in.ApprovedBy == "" {
			return c.Status(400).JSON(fiber.Map{"error": "approved_by is required"})
		}
		tag, err := pool.Exec(c.Context(),
			`UPDATE contents SET status='approved', approved_by=$1, approved_at=now()
			 WHERE id=$2 AND status='pending_approval'`, in.ApprovedBy, c.Params("id"))
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		if tag.RowsAffected() == 0 {
			return c.Status(409).JSON(fiber.Map{"error": "content not found or not pending approval"})
		}
		return c.JSON(fiber.Map{"status": "approved"})
	})

	log.Fatal(app.Listen(":8080"))
}
