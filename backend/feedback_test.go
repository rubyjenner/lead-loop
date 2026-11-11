package main

import (
	"strings"
	"testing"
)

func sample() pastResults {
	return pastResults{
		Total:     outcomeCount{Leads: 20, Hot: 5, Handed: 4, Closed: 2, Lost: 3},
		Channels:  []pastChannel{{"line", outcomeCount{12, 4, 3, 2, 1}}, {"web", outcomeCount{8, 1, 1, 0, 2}}},
		Campaigns: []pastCampaign{{"Launch", outcomeCount{20, 5, 4, 2, 3}}},
	}
}

func TestPastResultsNeedEnoughLeads(t *testing.T) {
	r := pastResults{Total: outcomeCount{Leads: 4, Hot: 4}}
	if r.render() != "" {
		t.Error("with fewer than 5 leads nothing should be rendered")
	}
	if !strings.Contains(r.planSection(), "ยังมีข้อมูลไม่พอ") {
		t.Errorf("plan section should say there is not enough data: %q", r.planSection())
	}
}

func TestPastResultsRender(t *testing.T) {
	out := sample().render()
	for _, want := range []string{"ลีด 20", "ลีดร้อน 5 (25%)", "อัตราปิดการขาย 40% ของลีดที่จบแล้ว", "ยังเปิดอยู่ 15", "ช่องทาง line", "แคมเปญ \"Launch\"", "ทุกสินค้ารวมกัน"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary misses %q:\n%s", want, out)
		}
	}
}

func TestBestChannel(t *testing.T) {
	if b := sample().best(); b != "line" {
		t.Errorf("best channel = %q, want line", b)
	}
	none := pastResults{Total: outcomeCount{Leads: 9}, Channels: []pastChannel{{"web", outcomeCount{Leads: 9}}}}
	if none.best() != "" {
		t.Error("no hot or closed leads: no best channel")
	}
}

func TestPlanUsesPastResults(t *testing.T) {
	ci := campaignInfo{Name: "Next", Past: sample()}
	if !strings.Contains(planPrompt(ci), "ผลของแคมเปญรอบก่อน") {
		t.Error("AI prompt should carry the earlier results")
	}
	if !strings.Contains(templatePlan(ci), "ช่องทางที่ได้ผลดีที่สุดคือ line") {
		t.Errorf("template plan should name the best channel: %q", templatePlan(ci))
	}
	if strings.Contains(planPrompt(campaignInfo{Name: "First"}), "ผลของแคมเปญรอบก่อน") {
		t.Error("first campaign has no earlier results to show")
	}
}

func TestSmallNumbersGetNoPercent(t *testing.T) {
	o := outcomeCount{Leads: 3, Hot: 1, Closed: 1, Lost: 1}
	l := o.line()
	if strings.Contains(l, "%") {
		t.Errorf("3 leads and 2 ended are too few for a percentage: %s", l)
	}
	if !strings.Contains(l, "ยังจบน้อยเกินกว่าจะสรุปอัตรา") {
		t.Errorf("should say the base is too small: %s", l)
	}
}

func TestTinyChannelIsNeverBest(t *testing.T) {
	r := pastResults{Total: outcomeCount{Leads: 12}, Channels: []pastChannel{
		{"line", outcomeCount{Leads: 9, Hot: 1}}, {"web", outcomeCount{Leads: 3, Hot: 3, Closed: 3}}}}
	if b := r.best(); b != "line" {
		t.Errorf("best = %q: a 3-lead channel must not win", b)
	}
}

func TestSameProductWording(t *testing.T) {
	r := sample()
	r.Scope = "same_product"
	if !strings.Contains(r.render(), "เฉพาะสินค้าเดียวกับแคมเปญนี้") {
		t.Error("summary should say it is limited to the same product")
	}
}
