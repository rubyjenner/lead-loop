package main

// Sends approved follow-up drafts to LINE customers.
//
// A draft becomes status='approved' on /followups/:id/approve (which already re-checks consent). This worker picks up
// approved drafts of LINE leads and pushes them. Rules:
//   - consent is re-checked in the SAME statement that claims the draft, so a customer who withdrew a moment ago is
//     never messaged (their draft is rejected instead); the database trigger also cancels unsent drafts on withdrawal
//   - LINE counts every push message against the Official Account's monthly quota, so nothing is sent when the quota
//     is used up. LINE_PUSH_RESERVE (default 20) messages are always kept for salespeople answering from the lead page
//   - a failed push goes back to 'approved' and is retried; after 3 failures the draft becomes 'failed'
//   - nothing runs unless LINE_CHANNEL_SECRET and LINE_CHANNEL_ACCESS_TOKEN are set
//
// GET /line/quota (any logged-in user) shows how many messages are left this month.

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	lineQuotaURL       = "https://api.line.me/v2/bot/message/quota"
	lineConsumptionURL = "https://api.line.me/v2/bot/message/quota/consumption"
	followupMaxTries   = 3
	followupPerTick    = 5
)

type lineQuota struct {
	Unlimited bool `json:"unlimited"`
	Limit     int  `json:"limit"`
	Used      int  `json:"used"`
}

func (q lineQuota) remaining() int {
	if q.Unlimited {
		return 1 << 30
	}
	if r := q.Limit - q.Used; r > 0 {
		return r
	}
	return 0
}

// followupAllowance is how many drafts may be sent now, keeping `reserve` messages for people answering by hand.
func followupAllowance(q lineQuota, reserve, perTick int) int {
	n := q.remaining() - reserve
	if n < 0 {
		return 0
	}
	if n > perTick {
		return perTick
	}
	return n
}

// quota reads the account's monthly message limit and how much is used (LINE's own numbers).
func (c *lineClient) quota(ctx context.Context) (lineQuota, error) {
	var q lineQuota
	data, err := c.do(ctx, http.MethodGet, lineQuotaURL, nil)
	if err != nil {
		return q, err
	}
	var lim struct {
		Type  string `json:"type"`
		Value int    `json:"value"`
	}
	if err := json.Unmarshal(data, &lim); err != nil {
		return q, err
	}
	q.Unlimited, q.Limit = lim.Type == "none", lim.Value
	data, err = c.do(ctx, http.MethodGet, lineConsumptionURL, nil)
	if err != nil {
		return q, err
	}
	var use struct {
		TotalUsage int `json:"totalUsage"`
	}
	if err := json.Unmarshal(data, &use); err != nil {
		return q, err
	}
	q.Used = use.TotalUsage
	return q, nil
}

type quotaCache struct {
	q  lineQuota
	at time.Time
}

func (qc *quotaCache) get(ctx context.Context, c *lineClient) (lineQuota, error) {
	if time.Since(qc.at) < time.Minute && !qc.at.IsZero() {
		return qc.q, nil
	}
	q, err := c.quota(ctx)
	if err != nil {
		return q, err
	}
	qc.q, qc.at = q, time.Now()
	return q, nil
}

func initLineFollowups(ctx context.Context, pool *pgxpool.Pool) error {
	for _, s := range []string{
		`ALTER TABLE lead_followups ADD COLUMN IF NOT EXISTS attempts INT NOT NULL DEFAULT 0`,
		`ALTER TABLE lead_followups ADD COLUMN IF NOT EXISTS last_error TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := pool.Exec(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

func registerLineFollowupRoutes(app fiber.Router, pool *pgxpool.Pool) {
	if err := initLineFollowups(context.Background(), pool); err != nil {
		log.Fatalf("line follow-up columns: %v", err)
	}
	cache := &quotaCache{}
	app.Get("/line/quota", func(c *fiber.Ctx) error {
		if !lineEnabled() {
			return c.JSON(fiber.Map{"enabled": false})
		}
		client := &lineClient{token: lineToken(), http: &http.Client{Timeout: 8 * time.Second}}
		q, err := cache.get(c.UserContext(), client)
		if err != nil {
			log.Printf("line quota: %v", err)
			return c.JSON(fiber.Map{"enabled": true, "known": false})
		}
		return c.JSON(fiber.Map{"enabled": true, "known": true, "unlimited": q.Unlimited, "limit": q.Limit,
			"used": q.Used, "remaining": q.remaining(), "reserve": envInt("LINE_PUSH_RESERVE", 20)})
	})
}

// runLineFollowupSender runs until ctx ends. Start it with: go runLineFollowupSender(ctx, pool)
func runLineFollowupSender(ctx context.Context, pool *pgxpool.Pool) {
	cache := &quotaCache{}
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if lineEnabled() {
				sendLineFollowups(ctx, pool, cache)
			}
		}
	}
}

func sendLineFollowups(ctx context.Context, pool *pgxpool.Pool, cache *quotaCache) {
	// drafts of customers who no longer agree are rejected, never sent (the trigger normally did this already)
	if _, err := pool.Exec(ctx, `UPDATE lead_followups f SET status='rejected', decided_by='system:consent_withdrawn', decided_at=now()
		FROM leads l WHERE f.lead_id=l.id AND f.status='approved' AND f.sent_at IS NULL AND l.channel='line'
		  AND (l.consent_general IS NOT TRUE OR l.consent_marketing IS NOT TRUE OR l.consent_withdrawn_at IS NOT NULL OR l.anonymized_at IS NOT NULL)`); err != nil {
		log.Printf("line followups cleanup: %v", err)
		return
	}
	var waiting int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM lead_followups f JOIN leads l ON l.id=f.lead_id
		WHERE f.status='approved' AND l.channel='line' AND f.attempts < $1`, followupMaxTries).Scan(&waiting); err != nil || waiting == 0 {
		return
	}
	client := &lineClient{token: lineToken(), http: &http.Client{Timeout: 8 * time.Second}}
	q, err := cache.get(ctx, client)
	if err != nil {
		log.Printf("line quota (follow-ups wait): %v", err)
		return
	}
	n := followupAllowance(q, envInt("LINE_PUSH_RESERVE", 20), followupPerTick)
	if n == 0 {
		log.Printf("line follow-ups waiting (%d) but the monthly quota is used up (%d/%d)", waiting, q.Used, q.Limit)
		return
	}
	for i := 0; i < n; i++ {
		sent, err := sendOneFollowup(ctx, pool, client)
		if err != nil {
			log.Printf("line follow-up: %v", err)
		}
		if !sent {
			return
		}
		cache.q.Used++
	}
}

// sendOneFollowup claims the oldest approved LINE draft (re-checking consent in the same statement) and pushes it.
// It returns false when there is nothing to send.
func sendOneFollowup(ctx context.Context, pool *pgxpool.Pool, client *lineClient) (bool, error) {
	var id, leadID int64
	var ext *string
	var body string
	err := pool.QueryRow(ctx, `
		UPDATE lead_followups f SET status='sent', sent_at=now(), attempts=f.attempts+1
		FROM leads l
		WHERE f.id = (SELECT f2.id FROM lead_followups f2 JOIN leads l2 ON l2.id=f2.lead_id
		              WHERE f2.status='approved' AND l2.channel='line' AND f2.attempts < $1
		              ORDER BY f2.id LIMIT 1 FOR UPDATE OF f2 SKIP LOCKED)
		  AND l.id = f.lead_id AND l.channel='line'
		  AND l.consent_general AND l.consent_marketing AND l.consent_withdrawn_at IS NULL AND l.anonymized_at IS NULL
		RETURNING f.id, f.lead_id, l.channel_user_id, f.body`, followupMaxTries).Scan(&id, &leadID, &ext, &body)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var pushErr error
	if ext == nil {
		pushErr = errors.New("lead has no LINE user id")
	} else {
		pushErr = client.push(ctx, *ext, lineText(body))
	}
	if pushErr != nil {
		_, _ = pool.Exec(ctx, `UPDATE lead_followups SET sent_at=NULL, last_error=$2,
			status = CASE WHEN attempts >= $3 THEN 'failed' ELSE 'approved' END WHERE id=$1`, id, trunc(pushErr.Error(), 300), followupMaxTries)
		return false, pushErr
	}
	_, _ = pool.Exec(ctx, `INSERT INTO audit_log (actor_email, action, target, detail) VALUES ('system:line-sender','followup_sent',$1,$2)`,
		"followup:"+strconv.FormatInt(id, 10), "lead:"+strconv.FormatInt(leadID, 10))
	return true, nil
}
