package main

// Results of earlier campaigns, fed back into the next plan. Only totals: no name, contact, message or
// lead id ever appears here, so it is safe to send to an AI provider.

import (
	"fmt"
	"strings"
)

// minFeedbackLeads: below this many leads the numbers say too little (and could single out a person).
const minFeedbackLeads = 5

type outcomeCount struct {
	Leads, Hot, Handed, Closed, Lost int
}

type pastCampaign struct {
	Name string
	outcomeCount
}

type pastChannel struct {
	Channel string
	outcomeCount
}

type pastResults struct {
	Total     outcomeCount
	Campaigns []pastCampaign // most recent first, already limited
	Channels  []pastChannel
}

func pct(part, whole int) string {
	if whole <= 0 {
		return "0%"
	}
	return fmt.Sprintf("%.0f%%", float64(part)*100/float64(whole))
}

func (o outcomeCount) line() string {
	return fmt.Sprintf("ลีด %d, ลีดร้อน %d (%s), ส่งต่อเซลส์ %d, ปิดการขายได้ %d (%s ของลีด), หลุด %d",
		o.Leads, o.Hot, pct(o.Hot, o.Leads), o.Handed, o.Closed, pct(o.Closed, o.Leads), o.Lost)
}

// enough reports whether there is enough history to be worth showing.
func (r pastResults) enough() bool { return r.Total.Leads >= minFeedbackLeads }

// render is the text added to the AI prompt and shown on the campaign page.
func (r pastResults) render() string {
	if !r.enough() {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("ผลของแคมเปญรอบก่อน (ตัวเลขรวม ไม่มีข้อมูลส่วนบุคคล):\n")
	fmt.Fprintf(&sb, "- รวม: %s\n", r.Total.line())
	for _, ch := range r.Channels {
		fmt.Fprintf(&sb, "- ช่องทาง %s: %s\n", ch.Channel, ch.line())
	}
	for _, c := range r.Campaigns {
		fmt.Fprintf(&sb, "- แคมเปญ \"%s\": %s\n", c.Name, c.line())
	}
	return sb.String()
}

// best returns the channel with the most closed sales (ties: most hot leads); "" when nothing closed or hot.
func (r pastResults) best() string {
	name, bc, bh := "", 0, 0
	for _, ch := range r.Channels {
		if ch.Closed > bc || (ch.Closed == bc && ch.Hot > bh) {
			name, bc, bh = ch.Channel, ch.Closed, ch.Hot
		}
	}
	if bc == 0 && bh == 0 {
		return ""
	}
	return name
}

// planSection is the part of the template plan (no AI) that restates the earlier results.
func (r pastResults) planSection() string {
	if !r.enough() {
		return "8. ผลรอบก่อน: ยังมีข้อมูลไม่พอ (ต้องมีลีดอย่างน้อย 5 ราย) แผนนี้จึงยังไม่อิงผลเดิม"
	}
	s := fmt.Sprintf("8. ผลรอบก่อน: %s", r.Total.line())
	if b := r.best(); b != "" {
		s += fmt.Sprintf(" ช่องทางที่ได้ผลดีที่สุดคือ %s ควรให้น้ำหนักช่องทางนี้ก่อน", b)
	}
	return s
}
