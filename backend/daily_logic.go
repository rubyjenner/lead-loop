package main

import "time"

// Pure rules for the daily job. No database and no HTTP here, so they can be unit tested on their own.

const (
	noReplyAfter   = 3 * 24 * time.Hour
	inactiveAfter  = 7 * 24 * time.Hour
	eventNoReply3d = "no_reply_3d"
	eventInactive  = "inactive_7d"
)

// silenceEvents says which "customer went quiet" events are due for one lead.
//   - lastActivity: the newest real event (or the lead's creation time)
//   - lastNoReply / lastInactive: when the system last recorded each event (nil = never)
//
// no_reply_3d fires once per quiet period (a new real event resets it).
// inactive_7d fires after 7 quiet days and again every further 7 days (the rule caps the total points).
func silenceEvents(now, lastActivity time.Time, lastNoReply, lastInactive *time.Time) []string {
	var out []string
	quiet := now.Sub(lastActivity)
	if quiet >= noReplyAfter && (lastNoReply == nil || lastNoReply.Before(lastActivity)) {
		out = append(out, eventNoReply3d)
	}
	if quiet >= inactiveAfter && (lastInactive == nil || lastInactive.Before(lastActivity) || now.Sub(*lastInactive) >= inactiveAfter) {
		out = append(out, eventInactive)
	}
	return out
}

// FollowupCfg is the per-level setting stored in scoring_settings (followup_cold / followup_warm).
type FollowupCfg struct {
	EveryDays int `json:"every_days"`
	Max       int `json:"max"`
}

// followupDue decides whether a follow-up draft should be created for a lead.
//   - count: follow-ups already created for this lead (any status, so a rejected draft is not re-created every day)
//   - lastAt: when the newest one was created (nil = none yet)
//   - hasPending: a draft is already waiting for a decision
func followupDue(now, lastActivity time.Time, cfg FollowupCfg, count int, lastAt *time.Time, hasPending bool) bool {
	if cfg.EveryDays <= 0 || cfg.Max <= 0 || count >= cfg.Max || hasPending {
		return false
	}
	gap := time.Duration(cfg.EveryDays) * 24 * time.Hour
	since := lastActivity
	if lastAt != nil && lastAt.After(since) {
		since = *lastAt
	}
	return now.Sub(since) >= gap
}

// followupBody is a fixed, non-personal template (no customer name, no AI call, so no personal data
// leaves the system). seq starts at 1.
func followupBody(level string, seq int) string {
	const stop = ` (พิมพ์ "หยุด" หากไม่ต้องการรับข้อความอีก)`
	switch level {
	case "warm":
		switch {
		case seq <= 1:
			return "สวัสดีค่ะ ทางร้านยังเก็บสินค้าที่คุณสนใจไว้ให้ หากมีคำถามเรื่องราคาหรือการผ่อนชำระ ทักมาได้เลยค่ะ" + stop
		case seq == 2:
			return "สวัสดีค่ะ ขอติดตามอีกครั้ง หากสนใจนัดดูสินค้าหรืออยากให้เจ้าหน้าที่โทรกลับ แจ้งได้เลยค่ะ" + stop
		default:
			return "สวัสดีค่ะ ข้อความสุดท้ายจากเรา หากยังสนใจสินค้า ทักมาเมื่อพร้อมได้ตลอดเลยค่ะ" + stop
		}
	default:
		if seq <= 1 {
			return "สวัสดีค่ะ ยังสนใจสินค้าอยู่ไหมคะ หากต้องการรายละเอียดหรือราคา พิมพ์ทักมาได้เลยค่ะ" + stop
		}
		return "สวัสดีค่ะ ขอส่งข้อความสุดท้าย หากต้องการข้อมูลเพิ่มเติมเมื่อไหร่ ทักมาได้ตลอดเลยค่ะ" + stop
	}
}

// bangkokDay returns the calendar day (YYYY-MM-DD) in Thailand time and whether it is past the run hour.
func bangkokDay(now time.Time, hour int) (string, bool) {
	t := now.In(time.FixedZone("ICT", 7*3600))
	return t.Format("2006-01-02"), t.Hour() >= hour
}
