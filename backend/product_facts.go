package main

// Product facts that the chat bot may quote to customers. Marketing fills them in per product:
//
//	variants         รุ่น / ขนาด / สี
//	product_type     แบบสินค้า
//	after_sales      บริการหลังการขาย (รับประกัน เคลม คืนสินค้า)
//	payment_methods  ชำระเงินด้วยอะไรได้บ้าง
//	installment_info ผ่อนได้ไหม (เงื่อนไข หรือ "ไม่รับผ่อน")
//
// (price and description already exist on the product.) The bot answers ONLY from these texts. A field left empty
// means "we do not know": the bot passes that question to a person instead of guessing.
//
//	GET /product-facts         list for every product (login)
//	PUT /product-facts/:id     marketing/admin, replaces the five texts of one product
//	PUT /products/:id          marketing/admin, edits name, description, price, brand info
//
// Wire-up: initProductFacts is called from registerStaffReplyPublic and the routes from registerStaffReplyRoutes,
// so main.go needs no new line.
//
// It also adds the new customer topics (asked_variant, asked_type, asked_warranty, asked_payment, ready_to_buy):
// keywords, scoring rules and the hand-off list are inserted ONCE (flag in seed_flags), so rules and keywords an
// admin later edits or deletes are never put back on restart.

import (
	"context"
	"log"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

const productFactMaxRunes = 500

type productFacts struct {
	ID              int64  `json:"id,omitempty"`
	Variants        string `json:"variants"`
	ProductType     string `json:"product_type"`
	AfterSales      string `json:"after_sales"`
	PaymentMethods  string `json:"payment_methods"`
	InstallmentInfo string `json:"installment_info"`
}

func (f productFacts) clean() (productFacts, bool) {
	fields := []*string{&f.Variants, &f.ProductType, &f.AfterSales, &f.PaymentMethods, &f.InstallmentInfo}
	for _, p := range fields {
		*p = strings.TrimSpace(*p)
		if len([]rune(*p)) > productFactMaxRunes {
			return f, false
		}
	}
	return f, true
}

func initProductFacts(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `ALTER TABLE products
		ADD COLUMN IF NOT EXISTS variants TEXT NOT NULL DEFAULT '',
		ADD COLUMN IF NOT EXISTS product_type TEXT NOT NULL DEFAULT '',
		ADD COLUMN IF NOT EXISTS after_sales TEXT NOT NULL DEFAULT '',
		ADD COLUMN IF NOT EXISTS payment_methods TEXT NOT NULL DEFAULT '',
		ADD COLUMN IF NOT EXISTS installment_info TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := seedChatTopics(ctx, pool); err != nil {
		log.Printf("seed chat topics (the bot still works, new topics just have no keywords yet): %v", err) // not fatal
	}
	return nil
}

func seedChatTopics(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS seed_flags (key TEXT PRIMARY KEY, created_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO seed_flags(key) VALUES ('chat_topics_v1') ON CONFLICT DO NOTHING`)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return nil // already seeded once
	}
	if _, err := tx.Exec(ctx, `INSERT INTO event_keywords (event_type, keyword) VALUES
		('asked_variant','รุ่น'),('asked_variant','ขนาด'),('asked_variant','ไซส์'),('asked_variant','size'),
		('asked_variant','สีอะไร'),('asked_variant','มีสี'),('asked_variant','สีไหน'),('asked_variant','กี่สี'),
		('asked_type','แบบไหน'),('asked_type','มีแบบ'),('asked_type','แบบอะไร'),('asked_type','ประเภท'),('asked_type','สไตล์'),
		('asked_warranty','ประกัน'),('asked_warranty','หลังการขาย'),('asked_warranty','เคลม'),('asked_warranty','ซ่อม'),
		('asked_warranty','เปลี่ยนคืน'),('asked_warranty','คืนสินค้า'),('asked_warranty','คืนเงิน'),
		('asked_payment','ชำระ'),('asked_payment','จ่ายยังไง'),('asked_payment','จ่ายด้วย'),('asked_payment','โอน'),
		('asked_payment','บัตรเครดิต'),('asked_payment','พร้อมเพย์'),('asked_payment','เก็บเงินปลายทาง'),
		('ready_to_buy','เอาเลย'),('ready_to_buy','สั่งซื้อ'),('ready_to_buy','ขอซื้อ'),('ready_to_buy','ซื้อเลย'),
		('ready_to_buy','สั่งเลย'),('ready_to_buy','จะซื้อ'),('ready_to_buy','อยากสั่ง'),('ready_to_buy','สั่งยังไง'),('ready_to_buy','ซื้อยังไง')
		ON CONFLICT DO NOTHING`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO scoring_rules (name, description, category, is_core, condition, points, mode, max_total, sort_order)
		SELECT v.name, v.description, 'intent', FALSE, v.cond::jsonb, v.points, 'once', NULL, v.sort_order
		FROM (VALUES
		  ('ถามรุ่น/ขนาด/สี', NULL::text, '{"event":"asked_variant"}', 5, 160),
		  ('ถามแบบสินค้า', NULL::text, '{"event":"asked_type"}', 5, 165),
		  ('ถามบริการหลังการขาย', NULL::text, '{"event":"asked_warranty"}', 5, 170),
		  ('ถามช่องทางชำระเงิน', NULL::text, '{"event":"asked_payment"}', 10, 175),
		  ('พร้อมสั่งซื้อ', 'ส่งต่อทันที', '{"event":"ready_to_buy"}', 30, 180)
		) AS v(name, description, cond, points, sort_order)
		WHERE NOT EXISTS (SELECT 1 FROM scoring_rules r WHERE r.name = v.name)`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE scoring_settings SET value = value || '["ready_to_buy"]'::jsonb
		WHERE key='immediate_handoff_events' AND jsonb_typeof(value)='array' AND NOT (value @> '["ready_to_buy"]'::jsonb)`); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func registerProductFactsRoutes(app fiber.Router, pool *pgxpool.Pool) {
	writers := requireRole("marketing", "admin")

	app.Get("/product-facts", func(c *fiber.Ctx) error {
		rows, err := pool.Query(c.Context(), `SELECT id, variants, product_type, after_sales, payment_methods, installment_info
			FROM products ORDER BY id`)
		if err != nil {
			return serverError(c, err)
		}
		defer rows.Close()
		out := []productFacts{}
		for rows.Next() {
			var f productFacts
			if err := rows.Scan(&f.ID, &f.Variants, &f.ProductType, &f.AfterSales, &f.PaymentMethods, &f.InstallmentInfo); err != nil {
				return serverError(c, err)
			}
			out = append(out, f)
		}
		return c.JSON(out)
	})

	// PUT /products/:id {"name","description","price","brand_info"}: edit a product after it was created
	app.Put("/products/:id", writers, productEditor(pool), func(c *fiber.Ctx) error {
		id, err := strconv.ParseInt(c.Params("id"), 10, 64)
		if err != nil || id <= 0 {
			return c.Status(400).JSON(fiber.Map{"error": "invalid product id"})
		}
		var in struct {
			Name        string   `json:"name"`
			Description string   `json:"description"`
			Price       *float64 `json:"price"`
			BrandInfo   string   `json:"brand_info"`
		}
		if err := c.BodyParser(&in); err != nil || strings.TrimSpace(in.Name) == "" {
			return c.Status(400).JSON(fiber.Map{"error": "name is required"})
		}
		if in.Price != nil && *in.Price < 0 {
			return c.Status(400).JSON(fiber.Map{"error": "price must not be negative"})
		}
		tag, err := pool.Exec(c.Context(), `UPDATE products SET name=$1, description=$2, price=$3, brand_info=$4 WHERE id=$5`,
			strings.TrimSpace(in.Name), strings.TrimSpace(in.Description), in.Price, strings.TrimSpace(in.BrandInfo), id)
		if err != nil {
			return serverError(c, err)
		}
		if tag.RowsAffected() == 0 {
			return c.Status(404).JSON(fiber.Map{"error": "product not found"})
		}
		auditUser(c, pool, "product_update", "product:"+strconv.FormatInt(id, 10), "")
		return c.JSON(fiber.Map{"ok": true})
	})

	app.Put("/product-facts/:id", writers, productEditor(pool), func(c *fiber.Ctx) error {
		id, err := strconv.ParseInt(c.Params("id"), 10, 64)
		if err != nil || id <= 0 {
			return c.Status(400).JSON(fiber.Map{"error": "invalid product id"})
		}
		var in productFacts
		if err := c.BodyParser(&in); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		f, ok := in.clean()
		if !ok {
			return c.Status(400).JSON(fiber.Map{"error": "แต่ละช่องยาวได้ไม่เกิน 500 ตัวอักษร"})
		}
		tag, err := pool.Exec(c.Context(), `UPDATE products SET variants=$1, product_type=$2, after_sales=$3,
			payment_methods=$4, installment_info=$5 WHERE id=$6`,
			f.Variants, f.ProductType, f.AfterSales, f.PaymentMethods, f.InstallmentInfo, id)
		if err != nil {
			return serverError(c, err)
		}
		if tag.RowsAffected() == 0 {
			return c.Status(404).JSON(fiber.Map{"error": "product not found"})
		}
		auditUser(c, pool, "product_facts_update", "product:"+strconv.FormatInt(id, 10), "")
		f.ID = id
		return c.JSON(f)
	})
}
