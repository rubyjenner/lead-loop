package main

// Daily job: finds customers who went quiet (no_reply_3d / inactive_7d), drafts follow-up messages
// (nurturing) and keeps a run history. Wire it up in main.go after registerLeadRoutes:
//
//	registerDailyRoutes(app, pool)
//	go runDailyScheduler(ctx, pool)
//
// Rules it follows:
//   - only leads with valid general consent (not withdrawn, not anonymized) are processed
//   - follow-ups need marketing consent and a person must approve each draft before it is sent
//   - the message text is a fixed template, so no personal data goes to any AI provider
//   - one run per Thailand calendar day (claimed in the daily_runs table), so a restart never repeats it

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type dailyAPI struct {
	pool  *pgxpool.Pool
	leads *leadAPI
}

// DailySummary is what one run reports (also stored as JSON in daily_runs).
type DailySummary struct {
	Day           string `json:"day"`
	Checked       int    `json:"checked"`
	NoReply3d     int    `json:"no_reply_3d"`
	Inactive7d    int    `json:"inactive_7d"`
	FollowupsMade int    `json:"followups_created"`
	Errors        int    `json:"errors"`
}

func initDaily(ctx context.Context, pool *pgxpool.Pool) error {
	for _, s := range []string{
		`CREATE TABLE IF NOT EXISTS daily_runs (
			day         DATE PRIMARY KEY,
			started_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
			finished_at TIMESTAMPTZ,
			triggered_by TEXT NOT NULL DEFAULT 'schedule',
			summary     JSONB NOT NULL DEFAULT '{}'
		)`,
		`CREATE TABLE IF NOT EXISTS lead_followups (
			id         BIGSERIAL PRIMARY KEY,
			lead_id    BIGINT NOT NULL REFERENCES leads(id),
			level      TEXT NOT NULL,
			seq        INT NOT NULL,
			body       TEXT NOT NULL,
			status     TEXT NOT NULL DEFAULT 'pending_approval', -- pending_approval | approved | sent | rejected
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			decided_by TEXT,
			decided_at TIMESTAMPTZ,
			sent_at    TIMESTAMPTZ
		)`,
		`CREATE INDEX IF NOT EXISTS lead_followups_lead_idx ON lead_followups(lead_id)`,
		`CREATE INDEX IF NOT EXISTS lead_followups_status_idx ON lead_followups(status)`,
		`ALTER TABLE lead_followups ADD COLUMN IF NOT EXISTS created_by TEXT NOT NULL DEFAULT 'system:daily_job'`,
		`ALTER TABLE lead_followups ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'daily_job'`,
		// PDPA: a draft must never outlive the customer's consent. When consent is withdrawn, marketing is switched
		// off ("หยุด"), or the lead is anonymized, every draft that has not been SENT yet (waiting for approval, or
		// approved but not yet picked up by a channel) is rejected on the spot. A trigger does it, so it holds for
		// every way consent can change (chat, LINE, API, deletion) and for any sender added later.
		`CREATE OR REPLACE FUNCTION lead_followups_cancel_on_consent() RETURNS trigger AS $$
		BEGIN
		  UPDATE lead_followups SET status='rejected', decided_by='system:consent_withdrawn', decided_at=now()
		   WHERE lead_id = NEW.id AND status IN ('pending_approval','approved') AND sent_at IS NULL;
		  RETURN NULL;
		END $$ LANGUAGE plpgsql`,
		`DROP TRIGGER IF EXISTS lead_followups_cancel ON leads`,
		`CREATE TRIGGER lead_followups_cancel AFTER UPDATE ON leads FOR EACH ROW
		 WHEN ((NEW.consent_general IS DISTINCT FROM OLD.consent_general
		     OR NEW.consent_marketing IS DISTINCT FROM OLD.consent_marketing
		     OR NEW.consent_withdrawn_at IS DISTINCT FROM OLD.consent_withdrawn_at
		     OR NEW.anonymized_at IS DISTINCT FROM OLD.anonymized_at)
		   AND (NEW.consent_general IS NOT TRUE OR NEW.consent_marketing IS NOT TRUE
		     OR NEW.consent_withdrawn_at IS NOT NULL OR NEW.anonymized_at IS NOT NULL))
		 EXECUTE FUNCTION lead_followups_cancel_on_consent()`,
		// drafts that already exist from before this trigger (or from a missed path) are cleaned once at start-up
		`UPDATE lead_followups f SET status='rejected', decided_by='system:consent_withdrawn', decided_at=now()
		  FROM leads l WHERE f.lead_id = l.id AND f.status IN ('pending_approval','approved') AND f.sent_at IS NULL
		   AND (l.consent_general IS NOT TRUE OR l.consent_marketing IS NOT TRUE
		     OR l.consent_withdrawn_at IS NOT NULL OR l.anonymized_at IS NOT NULL)`,
	} {
		if _, err := pool.Exec(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

func registerDailyRoutes(app fiber.Router, pool *pgxpool.Pool) {
	if err := initDaily(context.Background(), pool); err != nil {
		log.Fatalf("daily job tables: %v", err)
	}
	d := &dailyAPI{pool: pool, leads: &leadAPI{pool: pool}}
	mk := requireRole("marketing", "admin")
	adm := requireRole("admin")

	app.Post("/daily-jobs/run", adm, d.runNow)
	app.Get("/daily-jobs/last", adm, d.last)
	app.Get("/followups", d.listFollowups)
	app.Post("/leads/:id/followups", mk, d.createDraft)
	app.Post("/followups/:id/approve", mk, d.decide("approve"))
	app.Post("/followups/:id/reject", mk, d.decide("reject"))
}

// ---------- the run itself ----------

type dailyLead struct {
	id        int64
	level     string
	marketing bool
	activity  time.Time
	lastNoRep *time.Time
	lastInact *time.Time
	fuCount   int
	fuLast    *time.Time
	fuPending bool
}

func (d *dailyAPI) loadFollowupCfg(ctx context.Context) (map[string]FollowupCfg, error) {
	cfg := map[string]FollowupCfg{"cold": {EveryDays: 7, Max: 2}, "warm": {EveryDays: 3, Max: 3}}
	rows, err := d.pool.Query(ctx, `SELECT key, value FROM scoring_settings WHERE key IN ('followup_cold','followup_warm')`)
	if err != nil {
		return cfg, err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var v json.RawMessage
		if err := rows.Scan(&k, &v); err != nil {
			return cfg, err
		}
		var c FollowupCfg
		if json.Unmarshal(v, &c) == nil {
			cfg[k[len("followup_"):]] = c
		}
	}
	return cfg, rows.Err()
}

func (d *dailyAPI) loadCandidates(ctx context.Context) ([]dailyLead, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT l.id, l.level, l.consent_marketing,
		  GREATEST(l.created_at, COALESCE((SELECT max(e.created_at) FROM lead_events e
		       WHERE e.lead_id=l.id AND e.type NOT IN ('no_reply_3d','inactive_7d')), l.created_at)),
		  (SELECT max(e.created_at) FROM lead_events e WHERE e.lead_id=l.id AND e.type='no_reply_3d'),
		  (SELECT max(e.created_at) FROM lead_events e WHERE e.lead_id=l.id AND e.type='inactive_7d'),
		  (SELECT count(*) FROM lead_followups f WHERE f.lead_id=l.id),
		  (SELECT max(f.created_at) FROM lead_followups f WHERE f.lead_id=l.id),
		  EXISTS (SELECT 1 FROM lead_followups f WHERE f.lead_id=l.id AND f.status='pending_approval')
		FROM leads l
		WHERE l.anonymized_at IS NULL AND l.consent_general AND l.consent_withdrawn_at IS NULL
		  AND l.status IN ('new','nurturing','qualified')
		ORDER BY l.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []dailyLead
	for rows.Next() {
		var x dailyLead
		if err := rows.Scan(&x.id, &x.level, &x.marketing, &x.activity, &x.lastNoRep, &x.lastInact, &x.fuCount, &x.fuLast, &x.fuPending); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// runDaily does one pass. It is safe to call more than once: silence events and follow-ups are only
// created when they are really due.
func (d *dailyAPI) runDaily(ctx context.Context, now time.Time) (DailySummary, error) {
	day, _ := bangkokDay(now, 0)
	sum := DailySummary{Day: day}
	cfg, err := d.loadFollowupCfg(ctx)
	if err != nil {
		return sum, err
	}
	leads, err := d.loadCandidates(ctx)
	if err != nil {
		return sum, err
	}
	for _, l := range leads {
		sum.Checked++
		// 1) silence events (also for qualified leads: a hot lead that vanished should lose points)
		evs := silenceEvents(now, l.activity, l.lastNoRep, l.lastInact)
		if len(evs) > 0 {
			reqs := make([]leadEventReq, 0, len(evs))
			for _, e := range evs {
				reqs = append(reqs, leadEventReq{Type: e, Data: map[string]any{"source": "daily_job"}})
				if e == eventNoReply3d {
					sum.NoReply3d++
				} else {
					sum.Inactive7d++
				}
			}
			if _, _, err := d.leads.applyEvents(ctx, l.id, reqs); err != nil {
				log.Printf("daily: lead %d events: %v", l.id, err)
				sum.Errors++
				continue
			}
		}
		// 2) follow-up drafts: only cold/warm leads that agreed to marketing messages
		if !l.marketing || (l.level != "cold" && l.level != "warm") {
			continue
		}
		var cur string
		if err := d.pool.QueryRow(ctx, `SELECT level FROM leads WHERE id=$1 AND status IN ('new','nurturing')`, l.id).Scan(&cur); err != nil {
			continue // qualified, handed off, disqualified after rescoring: sales owns it now
		}
		if cur != "cold" && cur != "warm" {
			continue
		}
		if !followupDue(now, l.activity, cfg[cur], l.fuCount, l.fuLast, l.fuPending) {
			continue
		}
		if _, err := d.pool.Exec(ctx, `INSERT INTO lead_followups(lead_id, level, seq, body) VALUES($1,$2,$3,$4)`,
			l.id, cur, l.fuCount+1, followupBody(cur, l.fuCount+1)); err != nil {
			log.Printf("daily: lead %d follow-up: %v", l.id, err)
			sum.Errors++
			continue
		}
		var leadName string
		_ = d.pool.QueryRow(ctx, `SELECT display_name FROM leads WHERE id=$1`, l.id).Scan(&leadName)
		notifyRoles(ctx, d.pool, []string{"marketing", "admin"}, "followup_pending", "มีข้อความติดตามลีดรออนุมัติ", leadName, "/followups", "lead:"+strconv.FormatInt(l.id, 10))
		sum.FollowupsMade++
	}
	return sum, nil
}

// claimAndRun records the run for today and executes it. ok=false means today's run already exists.
func (d *dailyAPI) claimAndRun(ctx context.Context, now time.Time, by string, force bool) (DailySummary, bool, error) {
	day, _ := bangkokDay(now, 0)
	q := `INSERT INTO daily_runs(day, triggered_by) VALUES($1::text::date, $2) ON CONFLICT (day) DO NOTHING`
	if force {
		q = `INSERT INTO daily_runs(day, triggered_by) VALUES($1::text::date, $2)
		     ON CONFLICT (day) DO UPDATE SET started_at=now(), finished_at=NULL, triggered_by=EXCLUDED.triggered_by`
	}
	tag, err := d.pool.Exec(ctx, q, day, by)
	if err != nil {
		return DailySummary{}, false, err
	}
	if tag.RowsAffected() == 0 {
		return DailySummary{Day: day}, false, nil
	}
	sum, err := d.runDaily(ctx, now)
	if err != nil {
		return sum, true, err
	}
	b, _ := json.Marshal(sum)
	_, err = d.pool.Exec(ctx, `UPDATE daily_runs SET finished_at=now(), summary=$2 WHERE day=$1::text::date`, day, string(b))
	return sum, true, err
}

// runDailyScheduler checks every minute and runs once per Thailand day after DAILY_JOB_HOUR (default 9).
func runDailyScheduler(ctx context.Context, pool *pgxpool.Pool) {
	d := &dailyAPI{pool: pool, leads: &leadAPI{pool: pool}}
	hour := envInt("DAILY_JOB_HOUR", 9)
	if hour > 23 {
		hour = 9
	}
	if os.Getenv("DAILY_JOB_DISABLED") == "1" {
		log.Printf("daily job: disabled by DAILY_JOB_DISABLED")
		return
	}
	log.Printf("daily job: runs once a day after %02d:00 Thailand time", hour)
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			if _, due := bangkokDay(now, hour); !due {
				continue
			}
			sum, ran, err := d.claimAndRun(ctx, now, "schedule", false)
			if err != nil {
				log.Printf("daily job failed: %v", err)
			} else if ran {
				log.Printf("daily job done: %+v", sum)
			}
		}
	}
}

// ---------- HTTP ----------

func (d *dailyAPI) runNow(c *fiber.Ctx) error {
	email := ""
	if u := currentUser(c); u != nil {
		email = u.Email
	}
	sum, _, err := d.claimAndRun(c.Context(), time.Now(), "manual:"+email, true)
	if err != nil {
		return serverError(c, err)
	}
	b, _ := json.Marshal(sum)
	auditUser(c, d.pool, "daily_job_run", "daily_jobs", string(b))
	return c.JSON(sum)
}

func (d *dailyAPI) last(c *fiber.Ctx) error {
	var day time.Time
	var started time.Time
	var finished *time.Time
	var by string
	var raw []byte
	err := d.pool.QueryRow(c.Context(),
		`SELECT day, started_at, finished_at, triggered_by, summary FROM daily_runs ORDER BY day DESC LIMIT 1`).
		Scan(&day, &started, &finished, &by, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return c.JSON(fiber.Map{"run": nil})
	}
	if err != nil {
		return serverError(c, err)
	}
	var sum json.RawMessage = raw
	return c.JSON(fiber.Map{"run": fiber.Map{"day": day.Format("2006-01-02"), "started_at": started,
		"finished_at": finished, "triggered_by": by, "summary": sum}})
}

func (d *dailyAPI) listFollowups(c *fiber.Ctx) error {
	status := c.Query("status")
	rows, err := d.pool.Query(c.Context(), `
		SELECT f.id, f.lead_id, l.display_name, l.channel, f.level, f.seq, f.body, f.status,
		       f.created_at, f.decided_by, f.decided_at, f.sent_at, COALESCE(u.name,''), COALESCE(u.job_title,''), f.created_by, f.source, COALESCE(uc.name,''), COALESCE(uc.job_title,'')
		FROM lead_followups f JOIN leads l ON l.id = f.lead_id
		LEFT JOIN users u ON lower(u.email) = lower(f.decided_by)
		LEFT JOIN users uc ON lower(uc.email) = lower(f.created_by)
		WHERE l.anonymized_at IS NULL AND ($1 = '' OR f.status = $1)
		ORDER BY f.created_at DESC, f.id DESC LIMIT 200`, status)
	if err != nil {
		return serverError(c, err)
	}
	defer rows.Close()
	out := []fiber.Map{}
	for rows.Next() {
		var id, leadID int64
		var name, channel, level, body, st string
		var seq int
		var created time.Time
		var by *string
		var decided, sent *time.Time
		var byName, byTitle, createdBy, source, createdByName, createdByTitle string
		if err := rows.Scan(&id, &leadID, &name, &channel, &level, &seq, &body, &st, &created, &by, &decided, &sent, &byName, &byTitle, &createdBy, &source, &createdByName, &createdByTitle); err != nil {
			return serverError(c, err)
		}
		out = append(out, fiber.Map{"id": id, "lead_id": leadID, "display_name": name, "channel": channel,
			"level": level, "seq": seq, "body": body, "status": st, "created_at": created,
			"decided_by": by, "decided_by_name": byName, "decided_by_title": byTitle, "decided_at": decided, "sent_at": sent, "created_by": createdBy, "source": source, "created_by_name": createdByName, "created_by_title": createdByTitle})
	}
	return c.JSON(out)
}

// createDraft lets an assigned team member write a follow-up manually; it still requires approval and consent.
func (d *dailyAPI) createDraft(c *fiber.Ctx) error {
	leadID, err := c.ParamsInt("id")
	if err != nil {
		return leadErr(c, 400, "invalid lead id")
	}
	var in struct {
		Body string `json:"body"`
	}
	if err := c.BodyParser(&in); err != nil {
		return leadErr(c, 400, "invalid body")
	}
	body := strings.TrimSpace(in.Body)
	if body == "" || len([]rune(body)) > 2000 {
		return leadErr(c, 400, "message must be 1-2000 characters")
	}
	var level, channel string
	var consent bool
	err = d.pool.QueryRow(c.UserContext(), `SELECT level, channel, consent_general AND consent_marketing AND consent_withdrawn_at IS NULL AND anonymized_at IS NULL FROM leads WHERE id=$1`, leadID).Scan(&level, &channel, &consent)
	if errors.Is(err, pgx.ErrNoRows) {
		return leadErr(c, 404, "lead not found")
	}
	if err != nil {
		return serverError(c, err)
	}
	if !consent {
		return leadErr(c, 409, "lead has not consented to marketing messages")
	}
	var seq int
	if err := d.pool.QueryRow(c.UserContext(), `SELECT COALESCE(max(seq),0)+1 FROM lead_followups WHERE lead_id=$1`, leadID).Scan(&seq); err != nil {
		return serverError(c, err)
	}
	var id int64
	if err := d.pool.QueryRow(c.UserContext(), `INSERT INTO lead_followups(lead_id,level,seq,body,status,created_by,source) VALUES($1,$2,$3,$4,'pending_approval',$5,'manual') RETURNING id`, leadID, level, seq, body, currentUser(c).Email).Scan(&id); err != nil {
		return serverError(c, err)
	}
	auditUser(c, d.pool, "followup_draft_created", "followup:"+strconv.FormatInt(id, 10), "lead:"+strconv.FormatInt(int64(leadID), 10))
	var leadName string
	_ = d.pool.QueryRow(c.UserContext(), `SELECT display_name FROM leads WHERE id=$1`, leadID).Scan(&leadName)
	notifyApprovers(c.UserContext(), d.pool, []string{currentUser(c).Email}, "followup_pending", "มีข้อความติดตามลีดรออนุมัติ", leadName, "/followups", "followup:"+strconv.FormatInt(id, 10))
	return c.Status(201).JSON(fiber.Map{"id": id, "status": "pending_approval", "channel": channel, "created_by": currentUser(c).Email})
}

// decide approves or rejects a draft. Approval re-checks consent right now: a customer who withdrew
// since the draft was created must not receive it. The mock chat is "sent" at once; a real channel
// would pick up status=approved.
func (d *dailyAPI) decide(action string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id, err := c.ParamsInt("id")
		if err != nil {
			return leadErr(c, 400, "invalid follow-up id")
		}
		email := ""
		if u := currentUser(c); u != nil {
			email = u.Email
		}
		ctx := c.Context()
		if action == "reject" {
			tag, err := d.pool.Exec(ctx, `UPDATE lead_followups SET status='rejected', decided_by=$2, decided_at=now()
				WHERE id=$1 AND status='pending_approval'`, id, email)
			if err != nil {
				return serverError(c, err)
			}
			if tag.RowsAffected() == 0 {
				return leadErr(c, 409, "follow-up not found or not pending approval")
			}
			auditUser(c, d.pool, "followup_rejected", "followup:"+strconv.Itoa(id), "")
			var creator, leadName string
			_ = d.pool.QueryRow(ctx, `SELECT f.created_by,l.display_name FROM lead_followups f JOIN leads l ON l.id=f.lead_id WHERE f.id=$1`, id).Scan(&creator, &leadName)
			notifyUser(ctx, d.pool, creator, "followup_rejected", "ข้อความติดตามถูกปฏิเสธ", leadName, "/followups", "followup:"+strconv.Itoa(id)+":rejected")
			return c.JSON(fiber.Map{"status": "rejected"})
		}
		// ONE statement checks consent and approves, so a withdrawal that lands between a separate "check" and a
		// separate "update" can never slip through. (If one still lands right after, the trigger cancels the
		// approved draft because it has not been sent.) The mock chat is "sent" at once; a real channel picks up
		// status='approved' and MUST send with the same kind of single statement: re-check consent while claiming.
		var leadID int64
		var status string
		err = d.pool.QueryRow(ctx, `
			UPDATE lead_followups f SET
			  status = CASE WHEN l.channel = 'mock' THEN 'sent' ELSE 'approved' END,
			  decided_by = $2, decided_at = now(),
			  sent_at = CASE WHEN l.channel = 'mock' THEN now() END
			FROM leads l
			WHERE f.id = $1 AND f.status = 'pending_approval' AND l.id = f.lead_id
			  AND l.consent_general AND l.consent_marketing AND l.consent_withdrawn_at IS NULL AND l.anonymized_at IS NULL
			RETURNING f.status, f.lead_id`, id, email).Scan(&status, &leadID)
		if errors.Is(err, pgx.ErrNoRows) {
			// not approved: either there is no such pending draft, or the customer no longer agrees
			var pending bool
			if e := d.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM lead_followups WHERE id=$1 AND status='pending_approval')`, id).Scan(&pending); e != nil {
				return serverError(c, e)
			}
			if !pending {
				return leadErr(c, 409, "follow-up not found or not pending approval")
			}
			_, _ = d.pool.Exec(ctx, `UPDATE lead_followups SET status='rejected', decided_by='system:consent_withdrawn', decided_at=now()
				WHERE id=$1 AND status='pending_approval'`, id)
			return leadErr(c, 409, "lead no longer agrees to marketing messages; the draft was rejected")
		}
		if err != nil {
			return serverError(c, err)
		}
		auditUser(c, d.pool, "followup_"+status, "followup:"+strconv.Itoa(id), "lead:"+strconv.FormatInt(leadID, 10))
		var creator, leadName string
		_ = d.pool.QueryRow(ctx, `SELECT f.created_by,l.display_name FROM lead_followups f JOIN leads l ON l.id=f.lead_id WHERE f.id=$1`, id).Scan(&creator, &leadName)
		notifyUser(ctx, d.pool, creator, "followup_"+status, "ข้อความติดตามได้รับการอนุมัติแล้ว", leadName, "/followups", "followup:"+strconv.Itoa(id)+":"+status)
		return c.JSON(fiber.Map{"status": status})
	}
}
