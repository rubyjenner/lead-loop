package main

import "testing"

func TestCampaignStepsAndCurrent(t *testing.T) {
	cases := []struct {
		name string
		p    progressCounts
		cur  string
		note string
	}{
		{"brand new", progressCounts{}, "created", "ยังไม่มีแผน"},
		{"planned", progressCounts{HasPlan: true}, "planned", "ยังไม่มีคอนเทนต์"},
		{"drafting", progressCounts{HasPlan: true, Draft: 2}, "drafted", "ร่าง 2 ชิ้น ยังไม่ส่งอนุมัติ"},
		{"waiting for approval", progressCounts{HasPlan: true, Draft: 1, Pending: 2}, "submitted", "รออนุมัติ 2 ชิ้น"},
		{"approved, nothing to do", progressCounts{HasPlan: true, Approved: 2}, "approved", ""},
		{"rejected needs the creator", progressCounts{HasPlan: true, Approved: 1, Rejected: 1}, "submitted", "ถูกปฏิเสธ 1 ชิ้น รอผู้สร้างแก้"},
		{"leads arrive", progressCounts{HasPlan: true, Approved: 2, Leads: 5}, "leads", ""},
		{"handed off", progressCounts{HasPlan: true, Approved: 2, Leads: 5, Handed: 2}, "handed", ""},
		{"closed", progressCounts{HasPlan: true, Published: 2, Leads: 5, Handed: 2, Closed: 1}, "closed", ""},
		// LINE leads can arrive before anything is approved: the furthest step wins, the note still says what is open
		{"leads without approval", progressCounts{HasPlan: true, Draft: 1, Leads: 3}, "leads", "ร่าง 1 ชิ้น ยังไม่ส่งอนุมัติ"},
	}
	for _, c := range cases {
		steps := campaignSteps(c.p)
		if len(steps) != 8 {
			t.Fatalf("%s: want 8 steps, got %d", c.name, len(steps))
		}
		if got := steps[campaignCurrent(steps)].Key; got != c.cur {
			t.Errorf("%s: current = %s, want %s", c.name, got, c.cur)
		}
		if got := campaignNote(c.p); got != c.note {
			t.Errorf("%s: note = %q, want %q", c.name, got, c.note)
		}
	}
}

func TestCampaignStepDetails(t *testing.T) {
	s := campaignSteps(progressCounts{HasPlan: true, Approved: 2, Published: 1, Draft: 1, Leads: 4, Handed: 1})
	if s[2].Detail != "4 ชิ้น" || s[4].Detail != "3/4" || s[5].Detail != "4 ราย" || s[6].Detail != "1 ราย" {
		t.Errorf("unexpected details: %+v", s)
	}
	if e := campaignSteps(progressCounts{}); e[2].Detail != "" || e[5].Detail != "" {
		t.Errorf("empty campaign should have no counts: %+v", e)
	}
}

func TestRejectedContentIsNotApproved(t *testing.T) {
	s := campaignSteps(progressCounts{HasPlan: true, Approved: 1, Rejected: 1})
	if s[4].Done || !s[4].Warn || s[4].Detail != "1/2 · ปฏิเสธ 1" || !s[3].Done {
		t.Errorf("approved step with a rejection: %+v", s[3:5])
	}
	only := campaignSteps(progressCounts{HasPlan: true, Rejected: 1})
	if !only[3].Done || only[4].Done {
		t.Errorf("all rejected: submitted done, approved not: %+v", only[3:5])
	}
	if p := campaignSteps(progressCounts{HasPlan: true, Approved: 1, Pending: 1}); p[4].Done {
		t.Error("something still pending: not approved yet")
	}
	if ok := campaignSteps(progressCounts{HasPlan: true, Approved: 2}); !ok[4].Done || ok[4].Warn {
		t.Error("all approved: step done, no warning")
	}
}
