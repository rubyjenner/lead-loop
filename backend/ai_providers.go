package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// campaignInfo is the only data the AI (or the template fallback) is allowed to see:
// campaign + product facts. No customer or lead data goes through here.
type campaignInfo struct {
	Name        string
	Goal        string
	Audience    string
	Budget      float64
	Product     string
	ProductDesc string
	Brand       string
	Price       *float64 // nil when the campaign has no product or the product has no price
}

var errNoKey = errors.New("no API key configured")

var aiClient = &http.Client{Timeout: 30 * time.Second}

// defaultBudgetSplit is the starting budget split used by the template plan (the team can change it).
var defaultBudgetSplit = []struct {
	Channel string
	Pct     int
}{{"LINE", 40}, {"Facebook", 40}, {"อื่นๆ", 20}}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func aiMaxTokens() int {
	n, err := strconv.Atoi(envOr("AI_MAX_TOKENS", "1024"))
	if err != nil || n < 64 || n > 8192 {
		return 1024
	}
	return n
}

// aiProviders returns the provider order from AI_PROVIDERS (default: claude,gemini,template).
// Set AI_PROVIDERS=template to disable all external AI calls.
func aiProviders() []string {
	out := []string{}
	for _, p := range strings.Split(envOr("AI_PROVIDERS", "claude,gemini,template"), ",") {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// baht formats a number with thousands separators, e.g. 12900 -> "12,900".
func baht(v float64) string {
	s := strconv.FormatInt(int64(v+0.5), 10)
	n := len(s)
	if n <= 3 {
		return s
	}
	var b strings.Builder
	pre := n % 3
	if pre > 0 {
		b.WriteString(s[:pre])
		b.WriteString(",")
	}
	for i := pre; i < n; i += 3 {
		b.WriteString(s[i : i+3])
		if i+3 < n {
			b.WriteString(",")
		}
	}
	return b.String()
}

// facts renders the campaign/product facts as the user prompt context.
func (ci campaignInfo) facts() string {
	var b strings.Builder
	b.WriteString("ข้อมูลแคมเปญ\n")
	fmt.Fprintf(&b, "ชื่อแคมเปญ: %s\n", ci.Name)
	if ci.Goal != "" {
		fmt.Fprintf(&b, "เป้าหมาย: %s\n", ci.Goal)
	}
	if ci.Audience != "" {
		fmt.Fprintf(&b, "กลุ่มเป้าหมาย: %s\n", ci.Audience)
	}
	if ci.Budget > 0 {
		fmt.Fprintf(&b, "งบประมาณ: %s บาท\n", baht(ci.Budget))
	}
	if ci.Product == "" {
		b.WriteString("\nไม่มีข้อมูลสินค้าผูกกับแคมเปญนี้\n")
		return b.String()
	}
	b.WriteString("\nข้อมูลสินค้า\n")
	fmt.Fprintf(&b, "ชื่อสินค้า: %s\n", ci.Product)
	if ci.ProductDesc != "" {
		fmt.Fprintf(&b, "รายละเอียด: %s\n", ci.ProductDesc)
	}
	if ci.Price != nil {
		fmt.Fprintf(&b, "ราคา: %s บาท\n", baht(*ci.Price))
	}
	if ci.Brand != "" {
		fmt.Fprintf(&b, "ข้อมูลแบรนด์: %s\n", ci.Brand)
	}
	return b.String()
}

// generate tries each provider in AI_PROVIDERS order and falls back to the rule/template
// version. It never fails. The second return value is what produced the text
// ("claude", "gemini" or "template") and is stored in generated_by.
func generate(ctx context.Context, system, user string, fallback func() string) (string, string) {
	for _, p := range aiProviders() {
		var text string
		var err error
		switch p {
		case "claude":
			text, err = callClaude(ctx, system, user)
		case "gemini":
			text, err = callGemini(ctx, system, user)
		case "template":
			return fallback(), "template"
		default:
			continue
		}
		if err == nil {
			return strings.TrimSpace(text), p
		}
		if !errors.Is(err, errNoKey) {
			// log only the provider and the error, never the prompt (it may contain business data)
			log.Printf("ai provider %s failed, trying next: %v", p, err)
		}
	}
	return fallback(), "template"
}

func postJSON(ctx context.Context, url string, headers map[string]string, body any, out any) error {
	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := aiClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("status %d: %s", resp.StatusCode, trunc(string(data), 200))
	}
	return json.Unmarshal(data, out)
}

func callClaude(ctx context.Context, system, user string) (string, error) {
	key := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
	if key == "" {
		return "", errNoKey
	}
	var res struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	err := postJSON(ctx, "https://api.anthropic.com/v1/messages",
		map[string]string{"x-api-key": key, "anthropic-version": "2023-06-01"},
		map[string]any{
			"model":      envOr("ANTHROPIC_MODEL", "claude-haiku-5-5"),
			"max_tokens": aiMaxTokens(),
			"system":     system,
			"messages":   []map[string]string{{"role": "user", "content": user}},
		}, &res)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, part := range res.Content {
		if part.Type == "text" {
			b.WriteString(part.Text)
		}
	}
	if strings.TrimSpace(b.String()) == "" {
		return "", errors.New("empty response")
	}
	return b.String(), nil
}

func callGemini(ctx context.Context, system, user string) (string, error) {
	key := strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	if key == "" {
		return "", errNoKey
	}
	model := envOr("GEMINI_MODEL", "gemini-2.5-flash")
	var res struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	err := postJSON(ctx, "https://generativelanguage.googleapis.com/v1beta/models/"+model+":generateContent",
		map[string]string{"x-goog-api-key": key},
		map[string]any{
			"systemInstruction": map[string]any{"parts": []map[string]string{{"text": system}}},
			"contents": []map[string]any{
				{"role": "user", "parts": []map[string]string{{"text": user}}},
			},
			"generationConfig": map[string]any{"maxOutputTokens": aiMaxTokens()},
		}, &res)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if len(res.Candidates) > 0 {
		for _, part := range res.Candidates[0].Content.Parts {
			b.WriteString(part.Text)
		}
	}
	if strings.TrimSpace(b.String()) == "" {
		return "", errors.New("empty response")
	}
	return b.String(), nil
}

// ---------- rule/template fallback (uses only facts from the database) ----------

func orNeed(v, label string) string {
	if v != "" {
		return v
	}
	return "ยังไม่ได้ระบุ (ต้องการข้อมูลเพิ่ม: " + label + ")"
}

func templatePlan(ci campaignInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "แผนแคมเปญ: %s\n\n", ci.Name)
	b.WriteString("เป้าหมาย: " + orNeed(ci.Goal, "เป้าหมาย") + "\n")
	b.WriteString("กลุ่มเป้าหมาย: " + orNeed(ci.Audience, "กลุ่มเป้าหมาย") + "\n")
	if ci.Budget > 0 {
		fmt.Fprintf(&b, "งบประมาณ: %s บาท\n", baht(ci.Budget))
	} else {
		b.WriteString("งบประมาณ: ยังไม่ได้ระบุ (ต้องการข้อมูลเพิ่ม: งบประมาณ)\n")
	}
	if ci.Product != "" {
		fmt.Fprintf(&b, "สินค้า: %s", ci.Product)
		if ci.Price != nil {
			fmt.Fprintf(&b, " (ราคา %s บาท)", baht(*ci.Price))
		}
		b.WriteString("\n")
	} else {
		b.WriteString("สินค้า: ยังไม่ได้ผูกสินค้ากับแคมเปญ (ต้องการข้อมูลเพิ่ม: สินค้า)\n")
	}
	b.WriteString("\nช่องทางและสัดส่วนงบตั้งต้น (ทีมปรับได้):\n")
	for _, s := range defaultBudgetSplit {
		if ci.Budget > 0 {
			fmt.Fprintf(&b, "- %s %d%% (%s บาท)\n", s.Channel, s.Pct, baht(ci.Budget*float64(s.Pct)/100))
		} else {
			fmt.Fprintf(&b, "- %s %d%%\n", s.Channel, s.Pct)
		}
	}
	b.WriteString("\nข้อความหลัก: อธิบายสินค้าตามข้อมูลที่มี และชวนให้ติดต่อสอบถามทางแชต\n")
	b.WriteString("ตัวชี้วัด: reach, CTR, ต้นทุนต่อลีด, อัตราปิดการขาย\n")
	b.WriteString("\nหมายเหตุ: แผนนี้สร้างจากเทมเพลต ไม่ได้ใช้ AI ทีมควรตรวจและปรับก่อนใช้งาน\n")
	return b.String()
}

func templateContents(ci campaignInfo, channel string, count int) []string {
	name := ci.Product
	if name == "" {
		name = ci.Name
	}
	price := ""
	if ci.Price != nil {
		price = "ราคา " + baht(*ci.Price) + " บาท"
	}

	v1 := []string{name}
	if ci.ProductDesc != "" {
		v1 = append(v1, ci.ProductDesc)
	}
	if price != "" {
		v1 = append(v1, price)
	}
	v1 = append(v1, "สนใจสอบถามได้ทางแชต")

	v2 := "แนะนำ " + name
	if ci.Brand != "" {
		v2 += " จาก " + ci.Brand
	}
	if ci.ProductDesc != "" {
		v2 += ": " + ci.ProductDesc
	}

	v3 := "สนใจ " + name + "? ทักแชตเพื่อสอบถามราคาและรายละเอียดได้เลย"
	if price != "" {
		v3 = name + " " + price + " ทักแชตเพื่อสอบถามหรือนัดดูสินค้าได้เลย"
	}

	v4 := "[" + ci.Name + "] " + name
	if price != "" {
		v4 += " " + price
	}
	v4 += " ติดต่อสอบถามได้เลย"

	v5 := "ข้อมูลสินค้า: " + name
	if ci.ProductDesc != "" {
		v5 += " " + ci.ProductDesc
	}
	if price != "" {
		v5 += " " + price
	}

	variants := []string{strings.Join(v1, "\n"), v2, v3, v4, v5}
	out := make([]string, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, variants[i%len(variants)])
	}
	return out
}
