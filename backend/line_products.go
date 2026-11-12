package main

// LINE: let the customer choose a product, and send the product picture when the bot introduces a product.
//
//   - The customer taps a product button (or types "สินค้า"). The lead is attached to the campaign of that product,
//     so the lead page shows which product they asked about, and the bot answers from that product's facts.
//   - Offered products: campaigns that have a product and are "planned" or "running" (newest first, one per product),
//     plus the campaign in LINE_CAMPAIGN_ID. Finished and draft campaigns are never offered.
//   - The picture is the one marketing uploaded for the product. LINE shows only JPEG or PNG, at most 1 MB for the
//     preview, from a public https address. Anything else is skipped and the text is sent alone.
//     The address is LINE_IMAGE_BASE_URL (or APP_BASE_URL) + the product's image path, and only works while
//     PUBLIC_CHAT_ENABLED=1 (that is what makes /public/product-image/:id reachable).

import (
	"context"
	"errors"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

const (
	lineProductCmd       = "สินค้า"
	lineMaxProductButton = 12 // LINE allows 13 quick-reply buttons; one is kept for "withdraw consent"
	lineImageMaxBytes    = 1 << 20
)

// ---- pure helpers (unit tested) ----

type lineProductChoice struct {
	CampaignID int64
	Name       string
}

// lineIsProductCmd: the customer asks to see the products.
func lineIsProductCmd(text string) bool {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "สินค้า", "ดูสินค้า", "เลือกสินค้า", "เปลี่ยนสินค้า", "สินค้าอื่น", "products", "menu", "เมนู":
		return true
	}
	return false
}

// lineProductCampaign reads "action=product&campaign=37" from a button.
func lineProductCampaign(data string) (int64, bool) {
	v, err := url.ParseQuery(data)
	if err != nil || v.Get("action") != "product" {
		return 0, false
	}
	id, err := strconv.ParseInt(v.Get("campaign"), 10, 64)
	return id, err == nil && id > 0
}

// lineProductMenu is a text with one button per product and the "withdraw consent" button.
func lineProductMenu(text string, choices []lineProductChoice) lineMessage {
	if len(choices) > lineMaxProductButton {
		choices = choices[:lineMaxProductButton]
	}
	items := make([]map[string]any, 0, len(choices)+1)
	for _, c := range choices {
		items = append(items, map[string]any{"type": "postback", "label": lineTruncate(c.Name, 20),
			"data": "action=product&campaign=" + strconv.FormatInt(c.CampaignID, 10), "displayText": lineTruncate(c.Name, 300)})
	}
	items = append(items, map[string]any{"type": "message", "label": lineWithdrawCmd, "text": lineWithdrawCmd})
	return lineMessage{"type": "text", "text": lineTruncate(text, lineMaxText), "quickReply": lineQuick(items...)}
}

// lineImageMessage shows a picture. LINE needs an original and a preview address; the same file serves both.
func lineImageMessage(u string) lineMessage {
	return lineMessage{"type": "image", "originalContentUrl": u, "previewImageUrl": u}
}

// lineImageURL builds the public address of a product picture, or ok=false when LINE could not show it.
func lineImageURL(base, path, contentType string, size int) (string, bool) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if !strings.HasPrefix(base, "https://") || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return "", false
	}
	if contentType != "image/jpeg" && contentType != "image/png" {
		return "", false
	}
	if size <= 0 || size > lineImageMaxBytes {
		return "", false
	}
	u := base + path
	return u, len(u) <= 2000
}

// lineIntro is the text that introduces a product.
func lineIntro(p *chatProduct) string {
	if p == nil || strings.TrimSpace(p.Name) == "" {
		return "ขอบคุณที่สนใจค่ะ ถามราคาหรือรายละเอียดได้เลย"
	}
	t := "สนใจ " + strings.TrimSpace(p.Name) + " ใช่ไหมคะ"
	if d := strings.TrimSpace(p.Description); d != "" {
		t += "\n" + d
	}
	if p.Price != nil && *p.Price > 0 {
		t += "\nราคา " + chatBaht(*p.Price) + " บาท"
	}
	return t + "\nถามราคา รุ่น/สี การผ่อน หรือการชำระเงินได้เลยค่ะ พิมพ์ \"สินค้า\" เพื่อดูสินค้าอื่น"
}

// ---- database ----

// productChoices lists what a customer may pick.
func (ch *chatChannelAPI) productChoices(ctx context.Context, defaultCampaign *int64) ([]lineProductChoice, error) {
	var def int64
	if defaultCampaign != nil {
		def = *defaultCampaign
	}
	rows, err := ch.leads.pool.Query(ctx, `
		SELECT DISTINCT ON (c.product_id) c.id, p.name
		FROM campaigns c JOIN products p ON p.id = c.product_id
		WHERE c.status IN ('planned','running') OR c.id = $1
		ORDER BY c.product_id, (c.id = $1) DESC, c.id DESC`, def)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []lineProductChoice
	for rows.Next() {
		var x lineProductChoice
		if err := rows.Scan(&x.CampaignID, &x.Name); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CampaignID > out[j].CampaignID }) // newest campaign first
	return out, rows.Err()
}

// chooseProduct attaches the lead to the campaign of the product the customer picked. found=false when the
// campaign is not on offer (finished, no product, or does not exist).
func (ch *chatChannelAPI) chooseProduct(ctx context.Context, leadID, campaignID int64, defaultCampaign *int64) (bool, error) {
	var def int64
	if defaultCampaign != nil {
		def = *defaultCampaign
	}
	tag, err := ch.leads.pool.Exec(ctx, `
		UPDATE leads SET campaign_id=$1, updated_at=now()
		WHERE id=$2 AND anonymized_at IS NULL AND EXISTS (
		  SELECT 1 FROM campaigns c WHERE c.id=$1 AND c.product_id IS NOT NULL AND (c.status IN ('planned','running') OR c.id=$3))`,
		campaignID, leadID, def)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// productPicture returns the public address of the picture of the lead's product ("" = none or not usable).
func (ch *chatChannelAPI) productPicture(ctx context.Context, leadID int64) string {
	if !publicChatEnabled() {
		return ""
	}
	base := os.Getenv("LINE_IMAGE_BASE_URL")
	if strings.TrimSpace(base) == "" {
		base = os.Getenv("APP_BASE_URL")
	}
	var path, ctype string
	var size int
	err := ch.leads.pool.QueryRow(ctx, `
		SELECT p.image_url, i.content_type, octet_length(i.data)
		FROM leads l JOIN campaigns c ON c.id = l.campaign_id JOIN products p ON p.id = c.product_id
		JOIN product_images i ON i.product_id = p.id
		WHERE l.id = $1`, leadID).Scan(&path, &ctype, &size)
	if errors.Is(err, pgx.ErrNoRows) || err != nil {
		return ""
	}
	u, ok := lineImageURL(base, path, ctype, size)
	if !ok {
		return ""
	}
	return u
}
