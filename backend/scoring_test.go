package main

import (
	"encoding/json"
	"testing"
)

func intPtr(i int) *int { return &i }

func scoreTestRules() []ScoringRule {
	mk := func(id int64, name, cond string, pts int, mode string, max *int) ScoringRule {
		return ScoringRule{ID: id, Name: name, Condition: json.RawMessage(cond), Points: pts, Mode: mode, MaxTotal: max, Active: true, SortOrder: int(id)}
	}
	return []ScoringRule{
		mk(1, "ขอรายละเอียด", `{"event":"asked_details"}`, 10, "once", nil),
		mk(2, "ขอคุยกับเจ้าหน้าที่", `{"event":"requested_human"}`, 30, "once", nil),
		mk(3, "ถามราคา", `{"event":"asked_price"}`, 10, "once", nil),
		mk(4, "ขอให้ติดต่อกลับ", `{"event":"requested_callback"}`, 30, "once", nil),
		mk(5, "ถามสภาพ", `{"event":"asked_condition"}`, 10, "once", nil),
		mk(6, "ถามผ่อน", `{"event":"asked_installment"}`, 10, "once", nil),
		mk(7, "ถามจัดส่ง", `{"event":"asked_shipping"}`, 5, "once", nil),
		mk(8, "ต่อรอง", `{"event":"asked_negotiate"}`, 10, "once", nil),
		mk(9, "ขอนัดดู", `{"event":"requested_visit"}`, 30, "once", nil),
		mk(10, "ระบุงบ", `{"event":"gave_budget"}`, 15, "once", nil),
		mk(11, "งบพอ", `{"lead":{"field":"budget","op":">=","value":{"ref":"product.price"}}}`, 10, "once", nil),
		mk(12, "งบต่ำกว่า 70%", `{"lead":{"field":"budget","op":"<","value":{"ref":"product.price","multiply":0.7}}}`, -10, "once", nil),
		mk(13, "ตอบเร็ว", `{"event":"replied_fast","where":{"field":"minutes","op":"<=","value":60}}`, 5, "each", intPtr(10)),
		mk(14, "กลับมาคุย", `{"event":"returned_after_1d"}`, 10, "once", nil),
		mk(15, "เงียบ 3 วัน", `{"event":"no_reply_3d"}`, -15, "once", nil),
		mk(16, "ไม่เคลื่อนไหว 7 วัน", `{"event":"inactive_7d"}`, -10, "each", intPtr(30)),
	}
}

func scoreTestFacts(budget float64, price float64, events ...LeadEvent) *LeadFacts {
	f := &LeadFacts{Lead: map[string]any{}, Events: events}
	if budget > 0 { // unknown budget must be ABSENT, never 0 (else it looks like a "low budget")
		f.Lead["budget"] = budget
	}
	if price > 0 {
		f.ProductPrice = &price
	}
	return f
}

func scoreTestEvent(t string) LeadEvent { return LeadEvent{Type: t} }

func TestScenarios(t *testing.T) {
	s := DefaultScoringSettings()
	cases := []struct {
		name  string
		facts *LeadFacts
		score int
		level string
	}{
		{"ลูกค้าตัวจริง", scoreTestFacts(12900, 12900, scoreTestEvent("asked_price"), scoreTestEvent("gave_budget"), scoreTestEvent("requested_visit")), 65, "hot"},
		{"ใกล้ซื้อแต่ยังไม่นัด", scoreTestFacts(12900, 12900, scoreTestEvent("asked_price"), scoreTestEvent("asked_condition"), scoreTestEvent("asked_installment"), scoreTestEvent("gave_budget")), 55, "hot"},
		{"แค่ดูเฉยๆ", scoreTestFacts(10000, 12900, scoreTestEvent("asked_price"), scoreTestEvent("asked_condition"), scoreTestEvent("asked_installment"), scoreTestEvent("gave_budget")), 45, "warm"},
		{"งบไม่พอแล้วหาย", scoreTestFacts(5000, 12900, scoreTestEvent("asked_price"), scoreTestEvent("gave_budget"), scoreTestEvent("no_reply_3d")), 0, "cold"},
		{"ทักมาถามอย่างเดียว", scoreTestFacts(0, 12900, scoreTestEvent("asked_price"), LeadEvent{Type: "replied_fast", Data: map[string]any{"minutes": 10.0}}), 15, "cold"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := ScoreLead(scoreTestRules(), s, c.facts)
			if r.Score != c.score || r.Level != c.level {
				t.Fatalf("got %d/%s want %d/%s breakdown=%+v", r.Score, r.Level, c.score, c.level, r.Breakdown)
			}
		})
	}
}

func TestImmediateHandoffBelowThreshold(t *testing.T) {
	r := ScoreLead(scoreTestRules(), DefaultScoringSettings(), scoreTestFacts(0, 0, scoreTestEvent("asked_details"), scoreTestEvent("requested_callback")))
	if r.Score != 40 || !r.ImmediateHandoff || !r.Qualified || r.Level != "hot" {
		t.Fatalf("unexpected: %+v", r)
	}
}

func TestDisqualifyWins(t *testing.T) {
	r := ScoreLead(scoreTestRules(), DefaultScoringSettings(), scoreTestFacts(0, 0, scoreTestEvent("requested_visit"), scoreTestEvent("unsubscribed")))
	if !r.Disqualified || r.Qualified || r.Level != "disqualified" {
		t.Fatalf("unexpected: %+v", r)
	}
}

func TestEachModeCapsAndClamps(t *testing.T) {
	var evs []LeadEvent
	for i := 0; i < 10; i++ {
		evs = append(evs, LeadEvent{Type: "replied_fast", Data: map[string]any{"minutes": 5.0}})
	}
	r := ScoreLead(scoreTestRules(), DefaultScoringSettings(), scoreTestFacts(0, 0, evs...))
	if r.Score != 10 { // 10 x +5 capped at +10
		t.Fatalf("positive cap: got %d", r.Score)
	}
	var neg []LeadEvent
	for i := 0; i < 9; i++ {
		neg = append(neg, scoreTestEvent("inactive_7d"))
	}
	r = ScoreLead(scoreTestRules(), DefaultScoringSettings(), scoreTestFacts(0, 0, neg...))
	if r.Score != 0 { // -30 capped, then clamped to 0
		t.Fatalf("negative clamp: got %d", r.Score)
	}
}

func TestWhereFiltersEvents(t *testing.T) {
	slow := LeadEvent{Type: "replied_fast", Data: map[string]any{"minutes": 90.0}}
	r := ScoreLead(scoreTestRules(), DefaultScoringSettings(), scoreTestFacts(0, 0, slow))
	if r.Score != 0 {
		t.Fatalf("slow reply should not score: %+v", r)
	}
}

func TestMissingPriceMeansNoBudgetRules(t *testing.T) {
	r := ScoreLead(scoreTestRules(), DefaultScoringSettings(), scoreTestFacts(5000, 0)) // no product linked
	if r.Score != 0 {
		t.Fatalf("budget rules need a product price: %+v", r)
	}
}

func TestCombinators(t *testing.T) {
	cond := `{"all":[{"event":"asked_price"},{"any":[{"event":"asked_installment"},{"event":"asked_negotiate"}]},{"not":{"event":"no_reply_3d"}}]}`
	rules := []ScoringRule{{ID: 1, Name: "combo", Condition: json.RawMessage(cond), Points: 20, Mode: "once", Active: true}}
	s := DefaultScoringSettings()
	if r := ScoreLead(rules, s, scoreTestFacts(0, 0, scoreTestEvent("asked_price"), scoreTestEvent("asked_negotiate"))); r.Score != 20 {
		t.Fatalf("combo should match: %+v", r)
	}
	if r := ScoreLead(rules, s, scoreTestFacts(0, 0, scoreTestEvent("asked_price"), scoreTestEvent("asked_negotiate"), scoreTestEvent("no_reply_3d"))); r.Score != 0 {
		t.Fatalf("not() should block: %+v", r)
	}
	if r := ScoreLead(rules, s, scoreTestFacts(0, 0, scoreTestEvent("asked_price"))); r.Score != 0 {
		t.Fatalf("any() unmet: %+v", r)
	}
}

func TestInactiveAndBadRulesAreSkipped(t *testing.T) {
	rules := []ScoringRule{
		{ID: 1, Name: "off", Condition: json.RawMessage(`{"event":"asked_price"}`), Points: 50, Mode: "once", Active: false},
		{ID: 2, Name: "broken", Condition: json.RawMessage(`{"event":"a","lead":{"field":"x","op":"=","value":1}}`), Points: 50, Mode: "once", Active: true},
	}
	r := ScoreLead(rules, DefaultScoringSettings(), scoreTestFacts(0, 0, scoreTestEvent("asked_price")))
	if r.Score != 0 || len(r.Skipped) != 1 || r.Skipped[0] != "broken" {
		t.Fatalf("unexpected: %+v", r)
	}
}

func TestValidation(t *testing.T) {
	bad := map[string]string{
		"not json":        `{`,
		"empty":           `{}`,
		"two kinds":       `{"event":"a","all":[{"event":"b"}]}`,
		"where w/o event": `{"lead":{"field":"budget","op":">","value":1},"where":{"field":"x","op":"=","value":1}}`,
		"bad op":          `{"lead":{"field":"budget","op":"~=","value":1}}`,
		"no value":        `{"lead":{"field":"budget","op":">="}}`,
		"bad ref":         `{"lead":{"field":"budget","op":">=","value":{"ref":"user.password"}}}`,
		"too deep":        `{"not":{"not":{"not":{"not":{"not":{"event":"a"}}}}}}`,
	}
	for name, raw := range bad {
		if _, err := ParseCondition([]byte(raw)); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
	good := []string{
		`{"event":"asked_price"}`,
		`{"event":"replied_fast","where":{"field":"minutes","op":"<=","value":60}}`,
		`{"lead":{"field":"channel","op":"in","value":["line","web"]}}`,
		`{"not":{"not":{"not":{"not":{"event":"a"}}}}}`, // depth 5 is allowed
	}
	for _, raw := range good {
		if _, err := ParseCondition([]byte(raw)); err != nil {
			t.Errorf("%s: unexpected error %v", raw, err)
		}
	}
}

func TestInAndContains(t *testing.T) {
	f := &LeadFacts{Lead: map[string]any{"channel": "line", "note": "สนใจ iPhone 12 สีดำ"}}
	in := &CondPred{Field: "channel", Op: "in", Value: json.RawMessage(`["line","web"]`)}
	if !comparePred(in, f.Lead, f) {
		t.Fatal("in failed")
	}
	has := &CondPred{Field: "note", Op: "contains", Value: json.RawMessage(`"iphone"`)}
	if !comparePred(has, f.Lead, f) {
		t.Fatal("contains should be case-insensitive")
	}
}

func TestDetectEventsThai(t *testing.T) {
	kw := map[string][]string{
		"asked_details":      {"รายละเอียด", "ข้อมูลเพิ่มเติม"},
		"requested_human":    {"คุยกับเจ้าหน้าที่", "ขอคุยกับคน"},
		"asked_price":        {"ราคา", "เท่าไหร่"},
		"requested_callback": {"ติดต่อกลับ", "โทรกลับ"},
	}
	got := DetectEvents("สนใจครับ ขอรายละเอียดหน่อย ราคาเท่าไหร่ แล้วช่วยโทรกลับด้วย", kw)
	want := []string{"asked_details", "asked_price", "requested_callback"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	if len(DetectEvents("สวัสดีครับ", kw)) != 0 {
		t.Fatal("greeting should match nothing")
	}
}

func TestUnknownBudgetIsNotPenalised(t *testing.T) {
	r := ScoreLead(scoreTestRules(), DefaultScoringSettings(), scoreTestFacts(0, 12900, scoreTestEvent("asked_price")))
	if r.Score != 10 {
		t.Fatalf("lead without budget must not get the low-budget penalty: %+v", r)
	}
}
