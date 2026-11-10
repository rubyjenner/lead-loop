package main

// Product images for the AI (pure logic: no DB, no HTTP, standard library only):
//
//	go test image_logic.go image_logic_test.go
//
// The browser shrinks the picture before upload (about 1280 px, JPEG), so the limit here is small on purpose:
// the API body limit is 1 MB and the image travels inside JSON as base64.

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
)

const maxProductImageBytes = 600 << 10 // 600 KB after decoding

var (
	errImageEmpty   = errors.New("image is empty")
	errImageTooBig  = errors.New("image is too big")
	errImageBadType = errors.New("image must be JPEG, PNG or WebP")
)

// aiImage is a picture handed to an AI provider.
type aiImage struct {
	Mime string
	B64  string
}

// sniffImageMime looks at the first bytes, never at the file name or the type the client claims.
func sniffImageMime(b []byte) string {
	switch {
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return "image/jpeg"
	case len(b) >= 8 && bytes.Equal(b[:8], []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}):
		return "image/png"
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "image/webp"
	}
	return ""
}

// decodeImage accepts plain base64 or a data URL ("data:image/jpeg;base64,...") and returns the bytes and the real type.
func decodeImage(s string) ([]byte, string, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "data:") {
		i := strings.Index(s, ",")
		if i < 0 {
			return nil, "", errImageBadType
		}
		s = s[i+1:]
	}
	if s == "" {
		return nil, "", errImageEmpty
	}
	if base64.StdEncoding.DecodedLen(len(s)) > maxProductImageBytes+4 {
		return nil, "", errImageTooBig
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, "", errImageBadType
	}
	if len(b) == 0 {
		return nil, "", errImageEmpty
	}
	if len(b) > maxProductImageBytes {
		return nil, "", errImageTooBig
	}
	mime := sniffImageMime(b)
	if mime == "" {
		return nil, "", errImageBadType
	}
	return b, mime, nil
}

// imageFromRequest turns the optional "image" field of an AI request into an aiImage (nil = no picture).
func imageFromRequest(field string) (*aiImage, error) {
	if strings.TrimSpace(field) == "" {
		return nil, nil
	}
	b, mime, err := decodeImage(field)
	if err != nil {
		return nil, err
	}
	return &aiImage{Mime: mime, B64: base64.StdEncoding.EncodeToString(b)}, nil
}

// imagePromptNote is added to the system prompt only when a picture is sent. The AI may describe what is visible
// but must not turn the picture into facts (spec, condition, price) that the product data does not contain.
const imagePromptNote = "\nมีรูปสินค้าแนบมาด้วย ใช้รูปเพื่อบรรยายลักษณะภายนอกที่มองเห็นได้จริงเท่านั้น (เช่น สี รูปทรง องค์ประกอบในภาพ) " +
	"ห้ามเดาสเปก สภาพ รุ่น หรือราคาจากรูป และห้ามระบุข้อมูลที่ไม่มีในข้อมูลสินค้า"

func base64Std(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
