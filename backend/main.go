package main

import (
	"context"
	"errors"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/helmet"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Campaign struct {
	ID             int64     `json:"id"`
	ProductID      *int64    `json:"product_id"`
	Name           string    `json:"name"`
	Goal           string    `json:"goal"`
	TargetAudience string    `json:"target_audience"`
	Budget         float64   `json:"budget"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
}

type Product struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Price       *float64  `json:"price"`
	BrandInfo   string    `json:"brand_info"`
	CreatedAt   time.Time `json:"created_at"`
}

type Content struct {
	ID          int64      `json:"id"`
	CampaignID  int64      `json:"campaign_id"`
	Channel     string     `json:"channel"`
	Body        string     `json:"body"`
	Status      string     `json:"status"`
	ApprovedBy  *string    `json:"approved_by"`
	ApprovedAt  *time.Time `json:"approved_at"`
	PublishAt   *time.Time `json:"publish_at"`
	CreatedAt   time.Time  `json:"created_at"`
	GeneratedBy string     `json:"generated_by"` // human, claude, gemini or template
	CreatedBy   *string    `json:"created_by"`   // who created it (the approver must be someone else)
}

const contentCols = `id, campaign_id, channel, body, status::text, approved_by, approved_at, publish_at, created_at, generated_by, created_by`

func scanContent(row interface{ Scan(...any) error }) (Content, error) {
	var x Content
	err := row.Scan(&x.ID, &x.CampaignID, &x.Channel, &x.Body, &x.Status,
		&x.ApprovedBy, &x.ApprovedAt, &x.PublishAt, &x.CreatedAt, &x.GeneratedBy, &x.CreatedBy)
	return x, err
}

func main() {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	if err := initAuth(ctx, pool); err != nil {
		log.Fatal(err)
	}
	if err := initAI(ctx, pool); err != nil {
		log.Fatal(err)
	}

	cfg := fiber.Config{BodyLimit: 1 * 1024 * 1024} // reject request bodies over 1 MB
	// Only set PROXY_HEADER (e.g. X-Forwarded-For) when the API is reachable ONLY through a reverse proxy;
	// otherwise clients could fake their IP and dodge the login limiter.
	if h := os.Getenv("PROXY_HEADER"); h != "" {
		cfg.ProxyHeader = h
	}
	app := fiber.New(cfg)

	app.Use(helmet.New()) // security headers
	// CORS is off unless CORS_ORIGINS lists the allowed web origins (comma separated, no wildcard).
	if origins := os.Getenv("CORS_ORIGINS"); origins != "" {
		app.Use(cors.New(cors.Config{
			AllowOrigins: origins,
			AllowMethods: "GET,POST,PATCH,PUT,DELETE,OPTIONS",
			AllowHeaders: "Authorization,Content-Type",
			MaxAge:       600,
		}))
	}

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

	// Login is public; everything registered after this requires a valid token.
	registerAuthRoutes(app, pool)
	registerLeadRoutes(app, pool)
	writers := requireRole("marketing", "admin") // sales and others are read-only

	// Step 1: team creates a campaign from product info + goal + budget
	app.Post("/campaigns", writers, func(c *fiber.Ctx) error {
		var in Campaign
		if err := c.BodyParser(&in); err != nil || in.Name == "" {
			return c.Status(400).JSON(fiber.Map{"error": "name is required"})
		}
		if in.ProductID != nil {
			var ok bool
			if err := pool.QueryRow(c.Context(),
				`SELECT EXISTS (SELECT 1 FROM products WHERE id=$1)`, *in.ProductID).Scan(&ok); err != nil {
				return serverError(c, err)
			}
			if !ok {
				return c.Status(404).JSON(fiber.Map{"error": "product not found"})
			}
		}
		err := pool.QueryRow(c.Context(),
			`INSERT INTO campaigns (product_id, name, goal, target_audience, budget)
			 VALUES ($1,$2,$3,$4,$5) RETURNING id, status, created_at`,
			in.ProductID, in.Name, in.Goal, in.TargetAudience, in.Budget,
		).Scan(&in.ID, &in.Status, &in.CreatedAt)
		if err != nil {
			return serverError(c, err)
		}
		return c.Status(201).JSON(in)
	})

	app.Get("/campaigns", func(c *fiber.Ctx) error {
		rows, err := pool.Query(c.Context(),
			`SELECT id, product_id, name, goal, target_audience, budget::float8, status::text, created_at
			 FROM campaigns ORDER BY id DESC`)
		if err != nil {
			return serverError(c, err)
		}
		defer rows.Close()
		out := []Campaign{}
		for rows.Next() {
			var x Campaign
			if err := rows.Scan(&x.ID, &x.ProductID, &x.Name, &x.Goal, &x.TargetAudience, &x.Budget, &x.Status, &x.CreatedAt); err != nil {
				return serverError(c, err)
			}
			out = append(out, x)
		}
		return c.JSON(out)
	})

	// Step 1: product and brand info the AI will use (and must not invent beyond)
	app.Post("/products", writers, func(c *fiber.Ctx) error {
		var in Product
		if err := c.BodyParser(&in); err != nil || in.Name == "" {
			return c.Status(400).JSON(fiber.Map{"error": "name is required"})
		}
		if in.Price != nil && *in.Price < 0 {
			return c.Status(400).JSON(fiber.Map{"error": "price must not be negative"})
		}
		err := pool.QueryRow(c.Context(),
			`INSERT INTO products (name, description, price, brand_info)
			 VALUES ($1,$2,$3,$4) RETURNING id, created_at`,
			in.Name, in.Description, in.Price, in.BrandInfo,
		).Scan(&in.ID, &in.CreatedAt)
		if err != nil {
			return serverError(c, err)
		}
		return c.Status(201).JSON(in)
	})

	app.Get("/products", func(c *fiber.Ctx) error {
		rows, err := pool.Query(c.Context(),
			`SELECT id, name, description, price::float8, brand_info, created_at
			 FROM products ORDER BY id DESC`)
		if err != nil {
			return serverError(c, err)
		}
		defer rows.Close()
		out := []Product{}
		for rows.Next() {
			var x Product
			if err := rows.Scan(&x.ID, &x.Name, &x.Description, &x.Price, &x.BrandInfo, &x.CreatedAt); err != nil {
				return serverError(c, err)
			}
			out = append(out, x)
		}
		return c.JSON(out)
	})

	// Step 3: AI (or a person, for now) creates a content draft for a campaign
	app.Post("/campaigns/:id/contents", writers, func(c *fiber.Ctx) error {
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
			return serverError(c, err)
		}
		if !exists {
			return c.Status(404).JSON(fiber.Map{"error": "campaign not found"})
		}
		out, err := scanContent(pool.QueryRow(c.Context(),
			`INSERT INTO contents (campaign_id, channel, body, created_by) VALUES ($1,$2,$3,$4)
			 RETURNING `+contentCols, campaignID, in.Channel, in.Body, currentUser(c).Email))
		if err != nil {
			return serverError(c, err)
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
			return serverError(c, err)
		}
		defer rows.Close()
		out := []Content{}
		for rows.Next() {
			x, err := scanContent(rows)
			if err != nil {
				return serverError(c, err)
			}
			out = append(out, x)
		}
		return c.JSON(out)
	})

	// draft or rejected -> pending_approval
	app.Post("/contents/:id/submit", writers, func(c *fiber.Ctx) error {
		return setContentStatus(c, pool,
			`UPDATE contents SET status='pending_approval'
			 WHERE id=$1 AND status IN ('draft','rejected')`,
			"content not found or not in draft/rejected status", "pending_approval", "content_submitted")
	})

	// Step 4: a human approves content before it can be published
	app.Post("/contents/:id/approve", writers, func(c *fiber.Ctx) error {
		id, err := c.ParamsInt("id")
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid content id"})
		}
		// Company rule: the approver must be a different person from the creator of the content.
		var creator *string
		err = pool.QueryRow(c.Context(), `SELECT created_by FROM contents WHERE id=$1`, id).Scan(&creator)
		if errors.Is(err, pgx.ErrNoRows) {
			return c.Status(409).JSON(fiber.Map{"error": "content not found or not pending approval"})
		}
		if err != nil {
			return serverError(c, err)
		}
		if creator != nil && strings.EqualFold(*creator, currentUser(c).Email) {
			auditUser(c, pool, "content_approve_denied", "content:"+strconv.Itoa(id), "approver is the creator")
			return c.Status(403).JSON(fiber.Map{"error": "approver must be a different person from the content creator"})
		}
		tag, err := pool.Exec(c.Context(),
			`UPDATE contents SET status='approved', approved_by=$1, approved_at=now()
			 WHERE id=$2 AND status='pending_approval'`, currentUser(c).Email, id)
		if err != nil {
			return serverError(c, err)
		}
		if tag.RowsAffected() == 0 {
			return c.Status(409).JSON(fiber.Map{"error": "content not found or not pending approval"})
		}
		auditUser(c, pool, "content_approved", "content:"+strconv.Itoa(id), "")
		return c.JSON(fiber.Map{"status": "approved"})
	})

	app.Post("/contents/:id/reject", writers, func(c *fiber.Ctx) error {
		return setContentStatus(c, pool,
			`UPDATE contents SET status='rejected'
			 WHERE id=$1 AND status='pending_approval'`,
			"content not found or not pending approval", "rejected", "content_rejected")
	})

	// AI retry queue: when every provider failed and a template draft was saved, a worker tries the AI
	// again later (1 min, 5 min, 15 min, 1 h, 3 h) and upgrades the draft if nobody has touched it yet.
	queue := initQueue(ctx)
	registerAIRoutes(app, pool, queue)
	go runWorker(ctx, queue, upgradeHandler(pool), 15*time.Second, envInt("AI_RETRY_MAX", 5))

	log.Fatal(app.Listen(":8080"))
}

func setContentStatus(c *fiber.Ctx, pool *pgxpool.Pool, query, conflictMsg, status, auditAction string) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid content id"})
	}
	tag, err := pool.Exec(c.Context(), query, id)
	if err != nil {
		return serverError(c, err)
	}
	if tag.RowsAffected() == 0 {
		return c.Status(409).JSON(fiber.Map{"error": conflictMsg})
	}
	auditUser(c, pool, auditAction, "content:"+strconv.Itoa(id), "")
	return c.JSON(fiber.Map{"status": status})
}
