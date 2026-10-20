package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Guardrail: the AI may only use product/brand facts we give it.
const systemPrompt = `คุณเป็นผู้ช่วยการตลาดและการขาย ตอบเป็นภาษาไทย
กฎ: ใช้เฉพาะข้อมูลสินค้าและแบรนด์ที่ให้มาเท่านั้น ห้ามแต่งราคา โปรโมชัน หรือคุณสมบัติที่ไม่มีในข้อมูล
ห้ามโฆษณาเกินจริง ถ้าข้อมูลไม่พอให้บอกว่าต้องการข้อมูลเพิ่มอะไร`

var httpClient = &http.Client{Timeout: 60 * time.Second}

// callClaude calls the Claude Messages API. With no ANTHROPIC_API_KEY it returns mock
// (development mode), so the whole flow can be tested before a key is available.
// Retries on network errors, 429 and 5xx. Never logs prompts (may contain customer data).
func callClaude(ctx context.Context, system, user, mock string) (string, error) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return mock, nil
	}
	model := os.Getenv("ANTHROPIC_MODEL")
	if model == "" {
		model = "claude-sonnet-5-5"
	}
	body, err := json.Marshal(map[string]any{
		"model":      model,
		"max_tokens": 1500,
		"system":     system,
		"messages":   []map[string]string{{"role": "user", "content": user}},
	})
	if err != nil {
		return "", err
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(time.Duration(attempt) * 2 * time.Second):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			"https://api.anthropic.com/v1/messages", bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		req.Header.Set("x-api-key", key)
		req.Header.Set("anthropic-version", "2023-06-01")
		req.Header.Set("content-type", "application/json")

		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("claude api status %d", resp.StatusCode)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("claude api status %d", resp.StatusCode)
		}
		var out struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if err := json.Unmarshal(data, &out); err != nil {
			return "", err
		}
		var sb strings.Builder
		for _, b := range out.Content {
			if b.Type == "text" {
				sb.WriteString(b.Text)
			}
		}
		return strings.TrimSpace(sb.String()), nil
	}
	return "", fmt.Errorf("claude api unavailable after retries: %w", lastErr)
}

// campaignFacts loads a campaign with its product and renders the facts the AI may use.
func campaignFacts(ctx context.Context, pool *pgxpool.Pool, id int) (name, facts string, err error) {
	var (
		goal, audience, status string
		budget                 float64
		pname, pdesc, pbrand   *string
		pprice                 *float64
	)
	err = pool.QueryRow(ctx,
		`SELECT c.name, c.goal, c.target_audience, c.budget::float8, c.status::text,
		        p.name, p.description, p.price::float8, p.brand_info
		 FROM campaigns c LEFT JOIN products p ON p.id = c.product_id
		 WHERE c.id = $1`, id,
	).Scan(&name, &goal, &audience, &budget, &status, &pname, &pdesc, &pprice, &pbrand)
	if err != nil {
		return "", "", err
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "แคมเปญ: %s\nเป้าหมาย: %s\nกลุ่มเป้าหมาย: %s\nงบประมาณ: %.0f บาท\n", name, goal, audience, budget)
	if pname != nil {
		fmt.Fprintf(&sb, "สินค้า: %s\n", *pname)
		if pdesc != nil && *pdesc != "" {
			fmt.Fprintf(&sb, "รายละเอียดสินค้า: %s\n", *pdesc)
		}
		if pprice != nil {
			fmt.Fprintf(&sb, "ราคา: %.0f บาท\n", *pprice)
		}
		if pbrand != nil && *pbrand != "" {
			fmt.Fprintf(&sb, "ข้อมูลแบรนด์: %s\n", *pbrand)
		}
	} else {
		sb.WriteString("สินค้า: (ยังไม่ได้ระบุสินค้า)\n")
	}
	return name, sb.String(), nil
}

func registerAIRoutes(app *fiber.App, pool *pgxpool.Pool) {
	mockMode := func() bool { return os.Getenv("ANTHROPIC_API_KEY") == "" }

	// Step 2: AI plans the campaign (saved to campaigns.plan, status draft -> planned)
	app.Post("/campaigns/:id/plan", func(c *fiber.Ctx) error {
		id, err := c.ParamsInt("id")
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid campaign id"})
		}
		name, facts, err := campaignFacts(c.UserContext(), pool, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return c.Status(404).JSON(fiber.Map{"error": "campaign not found"})
		}
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		plan, err := callClaude(c.UserContext(), systemPrompt,
			"วางแผนแคมเปญสั้นๆ ประกอบด้วย กลุ่มลูกค้าย่อย ช่องทาง ข้อความหลัก และตัวชี้วัดที่ใช้วัดผล\n\n"+facts,
			"[MOCK] แผนตัวอย่างสำหรับแคมเปญ "+name+"\n1. กลุ่มลูกค้า: ตามที่กำหนด\n2. ช่องทาง: LINE\n3. ข้อความหลัก: (จำลอง)\n4. ตัวชี้วัด: reach, CTR, ต้นทุนต่อลีด")
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"error": "ai unavailable, try again later"})
		}
		if _, err := pool.Exec(c.UserContext(),
			`UPDATE campaigns SET plan=$1,
			   status = CASE WHEN status='draft' THEN 'planned'::campaign_status ELSE status END
			 WHERE id=$2`, plan, id); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"campaign_id": id, "plan": plan, "mock": mockMode()})
	})

	// Step 3: AI drafts content variants; saved as 'draft' (a human must still submit + approve)
	app.Post("/campaigns/:id/generate-contents", func(c *fiber.Ctx) error {
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

		name, facts, err := campaignFacts(c.UserContext(), pool, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return c.Status(404).JSON(fiber.Map{"error": "campaign not found"})
		}
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}

		mock := make([]string, in.Count)
		for i := range mock {
			mock[i] = fmt.Sprintf("[MOCK] ข้อความโฆษณาแบบที่ %d สำหรับแคมเปญ %s ช่องทาง %s", i+1, name, in.Channel)
		}
		text, err := callClaude(c.UserContext(), systemPrompt,
			fmt.Sprintf("เขียนข้อความโฆษณา %d แบบ สำหรับช่องทาง %s แต่ละแบบคั่นด้วยบรรทัดที่มีเฉพาะ --- ไม่ต้องมีหัวข้อหรือเลขข้อ\n\n%s",
				in.Count, in.Channel, facts),
			strings.Join(mock, "\n---\n"))
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"error": "ai unavailable, try again later"})
		}

		out := []Content{}
		for _, part := range strings.Split(text, "\n---\n") {
			part = strings.Trim(strings.TrimSpace(part), "-")
			part = strings.TrimSpace(part)
			if part == "" || len(out) >= in.Count {
				continue
			}
			x, err := scanContent(pool.QueryRow(c.UserContext(),
				`INSERT INTO contents (campaign_id, channel, body) VALUES ($1,$2,$3)
				 RETURNING `+contentCols, id, in.Channel, part))
			if err != nil {
				return c.Status(500).JSON(fiber.Map{"error": err.Error()})
			}
			out = append(out, x)
		}
		return c.Status(201).JSON(fiber.Map{"mock": mockMode(), "contents": out})
	})
}
