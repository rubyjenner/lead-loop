package main

// Product image upload. The image is stored in the database (table product_images), shown on the public storefront
// and next to the product in the back office. Only png, jpeg and webp up to 2 MB are accepted; the real file type is
// checked from the file's own bytes, not from the name or the type the browser claims. (SVG is refused on purpose.)
//
//	POST /products/:id/image {"data_url":"data:image/png;base64,..."}   marketing/admin (registered with registerThreadRoute)
//	GET  /public/product-image/:id                                       public, only when PUBLIC_CHAT_ENABLED=1
//
// The image is the shop's own product photo, never customer data. It is attached to the product and the storefront;
// when marketing asks the AI to plan a campaign or write content, the picture is also sent to the AI provider
// (ai_image.go); the upload page says so.

import (
	"context"
	"encoding/base64"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/jackc/pgx/v5/pgxpool"
)

const productImageMaxBytes = 2 << 20

var productImageDataURL = regexp.MustCompile(`^data:(image/(?:png|jpeg|webp));base64,([A-Za-z0-9+/]+={0,2})$`)

func initProductImages(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS product_images (
		product_id BIGINT PRIMARY KEY REFERENCES products(id) ON DELETE CASCADE,
		content_type TEXT NOT NULL,
		data BYTEA NOT NULL,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`)
	return err
}

// registerProductImagePublic: public GET, called from registerShopRoutes (before login middleware).
func registerProductImagePublic(app fiber.Router, pool *pgxpool.Pool) {
	if err := initProductImages(context.Background(), pool); err != nil {
		panic("product_images schema: " + err.Error())
	}
	imgLimit := limiter.New(limiter.Config{Max: 120, Expiration: time.Minute, LimitReached: func(c *fiber.Ctx) error {
		return c.Status(429).JSON(fiber.Map{"error": "too many requests"})
	}})
	app.Get("/public/product-image/:id", imgLimit, func(c *fiber.Ctx) error {
		if !publicChatEnabled() {
			return c.Status(404).JSON(fiber.Map{"error": "not found"})
		}
		id, err := strconv.ParseInt(c.Params("id"), 10, 64)
		if err != nil || id <= 0 {
			return c.Status(404).JSON(fiber.Map{"error": "not found"})
		}
		var ctype string
		var data []byte
		if err := pool.QueryRow(c.Context(), `SELECT content_type, data FROM product_images WHERE product_id=$1`, id).Scan(&ctype, &data); err != nil {
			return c.Status(404).JSON(fiber.Map{"error": "not found"})
		}
		c.Set("Content-Type", ctype)
		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("Cache-Control", "public, max-age=300")
		return c.Send(data)
	})
}

// registerProductImageUpload: needs login, called from registerStaffReplyRoutes (after registerAuthRoutes).
func registerProductImageUpload(app fiber.Router, pool *pgxpool.Pool) {
	writers := requireRole("marketing", "admin")
	app.Post("/products/:id/image", writers, productEditor(pool), func(c *fiber.Ctx) error {
		id, err := strconv.ParseInt(c.Params("id"), 10, 64)
		if err != nil || id <= 0 {
			return c.Status(400).JSON(fiber.Map{"error": "invalid product id"})
		}
		var in struct {
			DataURL string `json:"data_url"`
		}
		if err := c.BodyParser(&in); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "data_url is required"})
		}
		m := productImageDataURL.FindStringSubmatch(in.DataURL)
		if m == nil {
			return c.Status(400).JSON(fiber.Map{"error": "รองรับเฉพาะไฟล์ png, jpeg หรือ webp"})
		}
		data, err := base64.StdEncoding.DecodeString(m[2])
		if err != nil || len(data) == 0 {
			return c.Status(400).JSON(fiber.Map{"error": "ไฟล์รูปไม่ถูกต้อง"})
		}
		if len(data) > productImageMaxBytes {
			return c.Status(413).JSON(fiber.Map{"error": "รูปใหญ่เกิน 2 MB"})
		}
		if http.DetectContentType(data) != m[1] { // the bytes must really be the type that was declared
			return c.Status(400).JSON(fiber.Map{"error": "ไฟล์ไม่ตรงกับชนิดรูปที่ระบุ"})
		}
		var exists bool
		if err := pool.QueryRow(c.Context(), `SELECT EXISTS (SELECT 1 FROM products WHERE id=$1)`, id).Scan(&exists); err != nil {
			return serverError(c, err)
		}
		if !exists {
			return c.Status(404).JSON(fiber.Map{"error": "product not found"})
		}
		if _, err := pool.Exec(c.Context(), `INSERT INTO product_images (product_id, content_type, data) VALUES ($1,$2,$3)
			ON CONFLICT (product_id) DO UPDATE SET content_type=EXCLUDED.content_type, data=EXCLUDED.data, updated_at=now()`,
			id, m[1], data); err != nil {
			return serverError(c, err)
		}
		url := "/api/public/product-image/" + strconv.FormatInt(id, 10) + "?v=" + strconv.FormatInt(time.Now().Unix(), 10)
		if _, err := pool.Exec(c.Context(), `UPDATE products SET image_url=$1 WHERE id=$2`, url, id); err != nil {
			return serverError(c, err)
		}
		return c.Status(201).JSON(fiber.Map{"image_url": url})
	})
}
