package main

// The product picture as input for the AI. The picture itself is stored by product_image.go (table product_images);
// this file only reads it for AI requests. When marketing asks the AI to plan a campaign or write content, the AI
// gets (a) the picture attached to that one request, else (b) the saved picture of the campaign's product.
// Only the shop's own product photo ever goes to an AI provider, never anything about customers.

import (
	"context"
	"errors"
	"log"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// loadStoredImage returns the saved picture of the campaign's product, or nil (no product, no picture, or a read
// problem: the AI request must still work without it).
func loadStoredImage(ctx context.Context, pool *pgxpool.Pool, campaignID int) *aiImage {
	var data []byte
	err := pool.QueryRow(ctx, `SELECT i.data FROM campaigns c
		JOIN product_images i ON i.product_id = c.product_id WHERE c.id=$1`, campaignID).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		log.Printf("load product image for campaign %d: %v", campaignID, err)
		return nil
	}
	mime := sniffImageMime(data) // trust the bytes, not the stored label
	if mime == "" {
		return nil
	}
	return &aiImage{Mime: mime, B64: base64Std(data)}
}

// resolveAIImage picks the picture for one AI request. source says which: "upload", "product" or "" (none).
func resolveAIImage(ctx context.Context, pool *pgxpool.Pool, campaignID int, attached string) (img *aiImage, source string, err error) {
	if strings.TrimSpace(attached) != "" {
		img, err = imageFromRequest(attached)
		if err != nil {
			return nil, "", err
		}
		return img, "upload", nil
	}
	if img = loadStoredImage(ctx, pool, campaignID); img != nil {
		return img, "product", nil
	}
	return nil, "", nil
}

func imageErrText(err error) string {
	switch {
	case errors.Is(err, errImageTooBig):
		return "รูปใหญ่เกินไป (ไม่เกิน 600 KB หลังย่อ)"
	case errors.Is(err, errImageEmpty):
		return "ไม่พบข้อมูลรูป"
	default:
		return "รูปต้องเป็นไฟล์ JPEG, PNG หรือ WebP"
	}
}

// imageUsed tells the page whether the picture really reached an AI provider ("upload", "product" or ""). When the
// template wrote the text (no key, quota, providers down) the picture was not used, so the page must not claim it was.
func imageUsed(r Result, from string) string {
	if r.By == "claude" || r.By == "gemini" {
		return from
	}
	return ""
}
