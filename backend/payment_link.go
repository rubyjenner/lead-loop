package main

// "Send the payment link in the chat": a salesperson sends the customer's pay link as a normal staff reply
// (LINE push for LINE leads, the web chat for web leads). It reuses sendLeadChat, so the same consent and
// channel rules apply. The person presses the button, nothing is sent by the bot on its own, so the amount
// can be checked first.
//
//	POST /payments/:id/send-link   (sales, admin)

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

var thaiZone = time.FixedZone("ICT", 7*3600)

func payLinkMessage(ref, amount, base, token string, expires time.Time) string {
	return fmt.Sprintf("ลิงก์ชำระเงิน %s ยอด %s บาท\n%s/pay/%s\nลิงก์หมดอายุ %s น.",
		ref, amount, strings.TrimRight(base, "/"), token, expires.In(thaiZone).Format("02/01 15:04"))
}

func (a *paymentAPI) sendLink(c *fiber.Ctx) error {
	if !a.enabled {
		return leadErr(c, 409, "payments are not enabled (set PAYMENTS_ENABLED=1 and PROMPTPAY_ID)")
	}
	pid, err := c.ParamsInt("id")
	if err != nil || pid < 1 {
		return leadErr(c, 400, "invalid payment id")
	}
	base := strings.TrimSpace(os.Getenv("APP_BASE_URL"))
	if base == "" {
		return leadErr(c, 409, "ตั้งค่า APP_BASE_URL ก่อน เพื่อให้ลิงก์เปิดได้บนเครื่องของลูกค้า")
	}
	var leadID int64
	var amount, token, status string
	var expires time.Time
	err = a.pool.QueryRow(c.Context(), `SELECT lead_id, amount::text, token, status, expires_at FROM payments WHERE id=$1`, pid).
		Scan(&leadID, &amount, &token, &status, &expires)
	if err != nil {
		return leadErr(c, 404, "payment not found")
	}
	if status != "pending" || !expires.After(time.Now()) {
		return leadErr(c, 409, "รายการนี้ไม่ได้รอชำระหรือหมดเวลาแล้ว ส่งลิงก์ไม่ได้")
	}
	msg := payLinkMessage(paymentRef(int64(pid)), amount, base, token, expires)
	row, err := sendLeadChat(c, a.pool, leadID, msg)
	if row == nil {
		return err // refused (no consent, wrong channel, LINE failed); the response is written
	}
	if _, err := a.pool.Exec(c.Context(), `UPDATE payments SET link_sent_at=now() WHERE id=$1`, pid); err != nil {
		return serverError(c, err)
	}
	auditUser(c, a.pool, "payment_link_sent", "payment:"+strconv.Itoa(pid), "lead:"+strconv.FormatInt(leadID, 10))
	return c.JSON(fiber.Map{"sent": true, "sent_at": row.CreatedAt})
}
