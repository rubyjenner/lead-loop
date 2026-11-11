package main

import "testing"

func sp(s string) *string { return &s }

func TestMayEditCampaign(t *testing.T) {
	cases := []struct {
		name            string
		role, email     string
		creator         *string
		allowed, ovride bool
	}{
		{"creator edits own", "marketing", "a@x.com", sp("a@x.com"), true, false},
		{"case-insensitive", "marketing", "A@x.com", sp("a@X.com"), true, false},
		{"other marketing denied", "marketing", "b@x.com", sp("a@x.com"), false, false},
		{"legacy campaign open to marketing", "marketing", "b@x.com", nil, true, false},
		{"empty creator is legacy", "marketing", "b@x.com", sp(""), true, false},
		{"admin edits other: override", "admin", "root@x.com", sp("a@x.com"), true, true},
		{"admin edits own: no override", "admin", "root@x.com", sp("root@x.com"), true, false},
		{"admin edits legacy: no override", "admin", "root@x.com", nil, true, false},
		{"sales denied", "sales", "s@x.com", sp("s@x.com"), false, false},
		{"unknown role denied", "", "s@x.com", nil, false, false},
	}
	for _, c := range cases {
		a, o := mayEditCampaign(c.role, c.email, c.creator)
		if a != c.allowed || o != c.ovride {
			t.Errorf("%s: got (%v,%v) want (%v,%v)", c.name, a, o, c.allowed, c.ovride)
		}
	}
}

func TestMayEditProduct(t *testing.T) {
	cases := []struct {
		role       string
		used, mine int
		allowed    bool
		override   bool
	}{
		{"marketing", 0, 0, true, false},  // product being set up
		{"marketing", 2, 1, true, false},  // I made one of the campaigns
		{"marketing", 2, 0, false, false}, // only others use it
		{"admin", 2, 0, true, true},
		{"admin", 2, 1, true, false},
		{"admin", 0, 0, true, false},
		{"sales", 0, 0, false, false},
	}
	for _, c := range cases {
		a, o := mayEditProduct(c.role, c.used, c.mine)
		if a != c.allowed || o != c.override {
			t.Errorf("%+v: got (%v,%v)", c, a, o)
		}
	}
}

func TestMayDecide(t *testing.T) {
	if mayDecide("a@x.com", sp("A@x.com"), nil) {
		t.Error("content creator must not decide")
	}
	if mayDecide("a@x.com", sp("b@x.com"), sp("a@x.com")) {
		t.Error("campaign creator must not decide")
	}
	if !mayDecide("c@x.com", sp("b@x.com"), sp("a@x.com")) {
		t.Error("a third person may decide")
	}
	if !mayDecide("c@x.com", nil, nil) {
		t.Error("legacy content with no creators may be decided by anyone")
	}
}

func TestCleanReason(t *testing.T) {
	if _, ok := cleanReason("   "); ok {
		t.Error("blank reason accepted")
	}
	if r, ok := cleanReason("  แก้ราคา  "); !ok || r != "แก้ราคา" {
		t.Errorf("got %q %v", r, ok)
	}
	long := make([]rune, maxReasonLen+1)
	for i := range long {
		long[i] = 'ก'
	}
	if _, ok := cleanReason(string(long)); ok {
		t.Error("too long reason accepted")
	}
}

func TestPeopleLookup(t *testing.T) {
	p := people{"a@x.com": {Name: "สมชาย ใจดี", Title: "Marketing Manager"}}
	x := Content{CreatedBy: sp("A@X.com"), ApprovedBy: sp("zz@x.com")}
	p.fill(&x)
	if x.CreatedByName != "สมชาย ใจดี" || x.CreatedByTitle != "Marketing Manager" {
		t.Errorf("created: %+v", x)
	}
	if x.ApprovedByName != "" {
		t.Errorf("unknown person should stay empty: %+v", x)
	}
}
