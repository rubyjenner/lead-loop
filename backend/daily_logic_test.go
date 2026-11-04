package main

import (
	"strings"
	"testing"
	"time"
)

func tm(days float64) time.Time {
	base := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	return base.Add(time.Duration(days * 24 * float64(time.Hour)))
}

func TestSilenceEvents(t *testing.T) {
	act := tm(0)
	two := tm(2.9)
	if got := silenceEvents(two, act, nil, nil); len(got) != 0 {
		t.Fatalf("under 3 days: %v", got)
	}
	if got := silenceEvents(tm(3), act, nil, nil); len(got) != 1 || got[0] != eventNoReply3d {
		t.Fatalf("3 days: %v", got)
	}
	// already recorded after the last activity: do not repeat
	nr := tm(3)
	if got := silenceEvents(tm(4), act, &nr, nil); len(got) != 0 {
		t.Fatalf("no repeat: %v", got)
	}
	// customer replied at day 5, then went quiet again: fires again after 3 more days
	act2 := tm(5)
	if got := silenceEvents(tm(8), act2, &nr, nil); len(got) != 1 || got[0] != eventNoReply3d {
		t.Fatalf("new quiet period: %v", got)
	}
	// 7 days: both
	if got := silenceEvents(tm(7), act, &nr, nil); len(got) != 1 || got[0] != eventInactive {
		t.Fatalf("7 days with no_reply recorded: %v", got)
	}
	if got := silenceEvents(tm(7), act, nil, nil); len(got) != 2 {
		t.Fatalf("7 days fresh: %v", got)
	}
	// inactive again only after another 7 days
	in := tm(7)
	if got := silenceEvents(tm(10), act, &nr, &in); len(got) != 0 {
		t.Fatalf("inactive too soon: %v", got)
	}
	if got := silenceEvents(tm(14), act, &nr, &in); len(got) != 1 || got[0] != eventInactive {
		t.Fatalf("inactive after 14 days: %v", got)
	}
}

func TestFollowupDue(t *testing.T) {
	warm := FollowupCfg{EveryDays: 3, Max: 3}
	act := tm(0)
	if followupDue(tm(2), act, warm, 0, nil, false) {
		t.Fatal("too early")
	}
	if !followupDue(tm(3), act, warm, 0, nil, false) {
		t.Fatal("first follow-up due at 3 days")
	}
	last := tm(3)
	if followupDue(tm(5), act, warm, 1, &last, false) {
		t.Fatal("second too early")
	}
	if !followupDue(tm(6), act, warm, 1, &last, false) {
		t.Fatal("second due 3 days after the first")
	}
	if followupDue(tm(30), act, warm, 3, &last, false) {
		t.Fatal("max reached")
	}
	if followupDue(tm(30), act, warm, 0, nil, true) {
		t.Fatal("a pending draft blocks a new one")
	}
	if followupDue(tm(30), act, FollowupCfg{}, 0, nil, false) {
		t.Fatal("zero config disables follow-ups")
	}
	// the customer answered again after the last follow-up: wait a full gap from that reply
	reply := tm(7)
	if followupDue(tm(8), reply, warm, 1, &last, false) {
		t.Fatal("recent reply resets the wait")
	}
	if !followupDue(tm(10), reply, warm, 1, &last, false) {
		t.Fatal("due 3 days after the reply")
	}
}

func TestFollowupBody(t *testing.T) {
	for _, lv := range []string{"cold", "warm"} {
		for seq := 1; seq <= 3; seq++ {
			b := followupBody(lv, seq)
			if !strings.Contains(b, "หยุด") {
				t.Fatalf("%s %d lacks the opt-out hint", lv, seq)
			}
		}
	}
}

func TestBangkokDay(t *testing.T) {
	d, ok := bangkokDay(time.Date(2026, 11, 3, 1, 59, 0, 0, time.UTC), 9) // 08:59 in Bangkok
	if d != "2026-11-03" || ok {
		t.Fatalf("%s %v", d, ok)
	}
	d, ok = bangkokDay(time.Date(2026, 11, 3, 2, 0, 0, 0, time.UTC), 9) // 09:00
	if d != "2026-11-03" || !ok {
		t.Fatalf("%s %v", d, ok)
	}
	d, _ = bangkokDay(time.Date(2026, 11, 3, 18, 0, 0, 0, time.UTC), 9) // 01:00 next day
	if d != "2026-11-04" {
		t.Fatal(d)
	}
}
