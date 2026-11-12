package main

import (
	"strings"
	"testing"
)

func TestLineIsProductCmd(t *testing.T) {
	for _, s := range []string{"สินค้า", " ดูสินค้า ", "Menu", "เปลี่ยนสินค้า"} {
		if !lineIsProductCmd(s) {
			t.Errorf("%q should open the product menu", s)
		}
	}
	for _, s := range []string{"ราคาเท่าไหร่", "สินค้ามีรับประกันไหม", ""} {
		if lineIsProductCmd(s) {
			t.Errorf("%q must not open the product menu", s)
		}
	}
}

func TestLineProductCampaign(t *testing.T) {
	if id, ok := lineProductCampaign("action=product&campaign=37"); !ok || id != 37 {
		t.Fatalf("got %d %v", id, ok)
	}
	for _, d := range []string{"action=consent", "action=product", "action=product&campaign=x", "action=product&campaign=-3", "action=product&campaign=0"} {
		if _, ok := lineProductCampaign(d); ok {
			t.Errorf("%q must be refused", d)
		}
	}
}

func TestLineProductMenu(t *testing.T) {
	var cs []lineProductChoice
	for i := 1; i <= 20; i++ {
		cs = append(cs, lineProductChoice{CampaignID: int64(i), Name: "สินค้าที่มีชื่อยาวมากมากมากมากมาก"})
	}
	m := lineProductMenu("เลือก", cs)
	items := m["quickReply"].(map[string]any)["items"].([]map[string]any)
	if len(items) != lineMaxProductButton+1 { // products + withdraw
		t.Fatalf("items = %d", len(items))
	}
	first := items[0]["action"].(map[string]any)
	if len([]rune(first["label"].(string))) > 20 {
		t.Error("label longer than LINE's 20 characters")
	}
	last := items[len(items)-1]["action"].(map[string]any)
	if last["text"] != lineWithdrawCmd {
		t.Error("withdraw button must stay in the menu")
	}
}

func TestLineImageURL(t *testing.T) {
	ok := func(base, path, ct string, size int) bool { _, o := lineImageURL(base, path, ct, size); return o }
	if !ok("https://x.example/", "/api/public/product-image/3?v=1", "image/png", 1000) {
		t.Error("a normal picture must be allowed")
	}
	if u, _ := lineImageURL("https://x.example/", "/api/public/product-image/3", "image/jpeg", 10); u != "https://x.example/api/public/product-image/3" {
		t.Errorf("url = %q", u)
	}
	if ok("http://x.example", "/a", "image/png", 10) {
		t.Error("LINE needs https")
	}
	if ok("", "/a", "image/png", 10) {
		t.Error("no base address, no picture")
	}
	if ok("https://x.example", "/a", "image/webp", 10) {
		t.Error("LINE cannot show webp")
	}
	if ok("https://x.example", "/a", "image/png", lineImageMaxBytes+1) {
		t.Error("preview over 1 MB must be skipped")
	}
	if ok("https://x.example", "//evil.example/a", "image/png", 10) || ok("https://x.example", "a", "image/png", 10) {
		t.Error("the path must start with a single slash")
	}
	if ok("https://x.example", "/"+strings.Repeat("a", 2000), "image/png", 10) {
		t.Error("addresses over 2000 characters are refused by LINE")
	}
}

func TestLineIntro(t *testing.T) {
	price := 12900.0
	got := lineIntro(&chatProduct{Name: "iPhone 12", Description: "มือสอง สภาพ 90%", Price: &price})
	for _, w := range []string{"iPhone 12", "มือสอง", "12,900"} {
		if !strings.Contains(got, w) {
			t.Errorf("intro lacks %q: %s", w, got)
		}
	}
	if lineIntro(nil) == "" {
		t.Error("intro must not be empty")
	}
}
