package main

// Why a lead was lost. Only a fixed code is stored (never free text), so the counts can be fed into the next
// AI plan without any personal data. The reason is optional: old clients that send only a status still work.

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// lostReasons: code -> Thai label (also used by the frontend list and the AI prompt).
var lostReasons = map[string]string{
	"price":      "ราคาสูงเกินไป",
	"no_reply":   "ลูกค้าไม่ตอบกลับ",
	"competitor": "เลือกเจ้าอื่น",
	"no_need":    "ยังไม่ต้องการ/ไม่จำเป็น",
	"timing":     "จังหวะเวลาไม่เหมาะ",
	"other":      "อื่น ๆ",
}

// lostReasonOrder keeps the output stable.
var lostReasonOrder = []string{"price", "no_reply", "competitor", "no_need", "timing", "other"}

func validLostReason(code string) bool { _, ok := lostReasons[code]; return ok }

func initLostReason(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `ALTER TABLE leads ADD COLUMN IF NOT EXISTS lost_reason TEXT`)
	return err
}

type reasonCount struct {
	Code  string
	Count int
}

// lostReasonLine: "ราคาสูงเกินไป 3, ลูกค้าไม่ตอบกลับ 2". Shown only when enough leads were lost with a reason,
// so one or two leads can never be singled out.
const minReasonLost = 3

func lostReasonLine(rs []reasonCount) string {
	total := 0
	for _, r := range rs {
		total += r.Count
	}
	if total < minReasonLost {
		return ""
	}
	parts := []string{}
	for _, code := range lostReasonOrder {
		for _, r := range rs {
			if r.Code == code && r.Count > 0 {
				parts = append(parts, fmt.Sprintf("%s %d", lostReasons[code], r.Count))
			}
		}
	}
	return strings.Join(parts, ", ")
}

// topLostReason returns the most common reason code ("" when below the minimum). Ties: list order.
func topLostReason(rs []reasonCount) string {
	total, best, bc := 0, "", 0
	for _, r := range rs {
		total += r.Count
	}
	if total < minReasonLost {
		return ""
	}
	for _, code := range lostReasonOrder {
		for _, r := range rs {
			if r.Code == code && r.Count > bc {
				best, bc = code, r.Count
			}
		}
	}
	return best
}

// lostAdvice: what the plan should do about the most common reason.
var lostAdvice = map[string]string{
	"price":      "ควรสื่อคุณค่าและโปรโมชันให้ชัดตั้งแต่ข้อความแรก",
	"no_reply":   "ควรตามต่อให้เร็วขึ้นและใช้ข้อความติดตามที่สั้นกว่าเดิม",
	"competitor": "ควรเน้นจุดเด่นที่ต่างจากคู่แข่ง",
	"no_need":    "ควรคัดกลุ่มเป้าหมายให้แคบลง",
	"timing":     "ควรวางช่วงเวลาแคมเปญและการติดตามใหม่",
	"other":      "",
}

func loadLostReasons(ctx context.Context, pool *pgxpool.Pool, excludeCampaign int, product *int64) ([]reasonCount, error) {
	rows, err := pool.Query(ctx, `SELECT l.lost_reason, count(*) FROM leads l JOIN campaigns c ON c.id = l.campaign_id
		WHERE l.status='lost' AND l.lost_reason IS NOT NULL AND l.campaign_id <> $1
		  AND ($2::bigint IS NULL OR c.product_id = $2)`+notTest+` GROUP BY l.lost_reason`, excludeCampaign, product)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []reasonCount
	for rows.Next() {
		var r reasonCount
		if err := rows.Scan(&r.Code, &r.Count); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
