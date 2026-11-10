package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

var (
	testJPEG = append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{1}, 100)...)
	testPNG  = append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, bytes.Repeat([]byte{2}, 100)...)
	testWEBP = append([]byte("RIFF\x10\x00\x00\x00WEBP"), bytes.Repeat([]byte{3}, 100)...)
)

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func TestSniffImageMime(t *testing.T) {
	for want, data := range map[string][]byte{"image/jpeg": testJPEG, "image/png": testPNG, "image/webp": testWEBP} {
		if got := sniffImageMime(data); got != want {
			t.Errorf("sniff = %q, want %q", got, want)
		}
	}
	for name, data := range map[string][]byte{
		"html":     []byte("<html><script>alert(1)</script></html>"),
		"gif":      []byte("GIF89a......."),
		"svg":      []byte("<svg xmlns='http://www.w3.org/2000/svg'/>"),
		"empty":    nil,
		"short":    {0xFF, 0xD8},
		"riff-wav": []byte("RIFF\x10\x00\x00\x00WAVEfmt "),
	} {
		if got := sniffImageMime(data); got != "" {
			t.Errorf("%s must not be accepted, got %q", name, got)
		}
	}
}

func TestDecodeImageAcceptsPlainAndDataURL(t *testing.T) {
	for _, in := range []string{b64(testPNG), "data:image/png;base64," + b64(testPNG), "  " + b64(testPNG) + "\n"} {
		b, mime, err := decodeImage(in)
		if err != nil || mime != "image/png" || !bytes.Equal(b, testPNG) {
			t.Errorf("decode(%.30q) = %v %q %v", in, len(b), mime, err)
		}
	}
}

func TestDecodeImageTrustsContentNotClaim(t *testing.T) {
	// the data URL says jpeg but the bytes are html: must be refused (stored images are served back to browsers)
	_, _, err := decodeImage("data:image/jpeg;base64," + b64([]byte("<html>hi</html>")))
	if !errors.Is(err, errImageBadType) {
		t.Errorf("err = %v, want bad type", err)
	}
	// a real PNG labelled as jpeg is fine, and the detected type wins
	_, mime, err := decodeImage("data:image/jpeg;base64," + b64(testPNG))
	if err != nil || mime != "image/png" {
		t.Errorf("mime = %q err = %v", mime, err)
	}
}

func TestDecodeImageLimits(t *testing.T) {
	big := append([]byte{0xFF, 0xD8, 0xFF}, bytes.Repeat([]byte{9}, maxProductImageBytes)...) // 3 bytes over
	if _, _, err := decodeImage(b64(big)); !errors.Is(err, errImageTooBig) {
		t.Errorf("err = %v, want too big", err)
	}
	ok := append([]byte{0xFF, 0xD8, 0xFF}, bytes.Repeat([]byte{9}, maxProductImageBytes-3)...) // exactly the limit
	if _, _, err := decodeImage(b64(ok)); err != nil {
		t.Errorf("exactly at the limit must pass: %v", err)
	}
	for _, in := range []string{"", "   ", "data:image/png;base64,", "!!!not base64!!!", "data:no-comma"} {
		if _, _, err := decodeImage(in); err == nil {
			t.Errorf("decode(%q) should fail", in)
		}
	}
}

func TestImageFromRequest(t *testing.T) {
	if img, err := imageFromRequest(""); img != nil || err != nil {
		t.Errorf("no image = nil,nil; got %v %v", img, err)
	}
	img, err := imageFromRequest("data:image/jpeg;base64," + b64(testJPEG))
	if err != nil || img == nil || img.Mime != "image/jpeg" || img.B64 != b64(testJPEG) {
		t.Errorf("img = %+v err = %v", img, err)
	}
	if _, err := imageFromRequest("garbage"); err == nil {
		t.Error("garbage must fail")
	}
}

func TestImagePromptNoteForbidsInventing(t *testing.T) {
	for _, w := range []string{"ห้ามเดา", "มองเห็น"} {
		if !strings.Contains(imagePromptNote, w) {
			t.Errorf("note must contain %q", w)
		}
	}
}
