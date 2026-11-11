package main

import "testing"

func TestExtractBudget(t *testing.T) {
	yes := map[string]float64{
		"งบ 15,000": 15000,
		"งบ15000":   15000,
		"มีงบประมาณไม่เกิน 20000 บาท": 20000,
		"งบสัก 8 พัน":                 8000,
		"งบ 2 หมื่น":                  20000,
		"งบ 1.5 หมื่น":                15000,
		"งบ 3 แสน":                    300000,
		"ไม่เกิน 12,000 ครับ":         12000,
		"งบ ๑๐๐๐๐":                    10000,
		"งบ 20k":                      20000,
		"อยากได้ค่ะ งบประมาณประมาณ 9500 บาท": 9500,
	}
	for text, want := range yes {
		got, ok := ExtractBudget(text)
		if !ok || got != want {
			t.Errorf("%q: got %v %v, want %v", text, got, ok, want)
		}
	}
	for _, text := range []string{
		"ราคา 12900 บาทใช่ไหม", "โทรกลับ 0812345678", "สีดำ ไซส์ 42", "งบไม่ค่อยมีค่ะ", "ไม่เกินนี้ไหม", "งบ 5", "งบ 999999999999",
		"ขอบคุณค่ะ", "",
	} {
		if got, ok := ExtractBudget(text); ok {
			t.Errorf("%q must not give a budget, got %v", text, got)
		}
	}
}
