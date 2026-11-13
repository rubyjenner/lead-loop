package main

import (
	"strings"
	"testing"
)

func TestLostReasonLineNeedsEnough(t *testing.T) {
	if lostReasonLine([]reasonCount{{"price", 2}}) != "" {
		t.Error("fewer than 3 lost leads must show nothing")
	}
	got := lostReasonLine([]reasonCount{{"no_reply", 2}, {"price", 3}})
	if got != "ราคาสูงเกินไป 3, ลูกค้าไม่ตอบกลับ 2" {
		t.Errorf("line = %q", got)
	}
}

func TestTopLostReason(t *testing.T) {
	if topLostReason([]reasonCount{{"price", 1}, {"timing", 1}}) != "" {
		t.Error("below the minimum there is no top reason")
	}
	if topLostReason([]reasonCount{{"no_reply", 4}, {"price", 2}}) != "no_reply" {
		t.Error("top reason should be no_reply")
	}
	if topLostReason([]reasonCount{{"price", 2}, {"no_reply", 2}}) != "price" {
		t.Error("tie goes to list order")
	}
}

func TestPastResultsShowLostReasons(t *testing.T) {
	r := sample()
	r.LostReasons = []reasonCount{{"price", 3}}
	if !strings.Contains(r.render(), "สาเหตุที่ลีดหลุด: ราคาสูงเกินไป 3") {
		t.Errorf("render misses the reasons:\n%s", r.render())
	}
	if !strings.Contains(r.planSection(), "ควรสื่อคุณค่า") {
		t.Errorf("plan section misses the advice: %s", r.planSection())
	}
	if strings.Contains(sample().render(), "สาเหตุที่ลีดหลุด") {
		t.Error("no reasons -> no reason line")
	}
}

func TestValidLostReason(t *testing.T) {
	if !validLostReason("price") || validLostReason("rm -rf") || validLostReason("") {
		t.Error("only the fixed codes are valid")
	}
}
