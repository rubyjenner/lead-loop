package main

// Automatic deletion when the retention period ends (PDPA: keep personal data only as long as needed).
// The company has not decided the periods yet, so both are settings an admin changes on the "ระยะเก็บข้อมูล" page and
// both are OFF (0) until someone sets them. "Delete" means the same as the admin's "ลบข้อมูลระบุตัวตน" button: name,
// contact, channel id, budget, events and stored chat messages are removed; the lead row (score, status, consent
// evidence) stays for the reports.
//
//	inactive_days   anonymize leads with no activity for this many days (0 = off, otherwise 30..3650).
//	                Leads that sales is working on (qualified, handed_off) are never touched.
//	withdrawn_days  anonymize leads whose consent was withdrawn this many days ago (0 = off, otherwise 7..3650)
//
//	GET  /retention       (admin) settings, how many leads would be affected right now, last run
//	PUT  /retention       (admin) {"inactive_days":365,"withdrawn_days":30}
//	POST /retention/run   (admin) run now
//
// A scheduler runs it once a day after 03:00 Thailand time. Every anonymization is written to the audit log
// (actor "system:retention"). Env RETENTION_INACTIVE_DAYS / RETENTION_WITHDRAWN_DAYS give the starting values
// before an admin saves the page.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

const retentionBatch = 500

type retentionCfg struct {
	InactiveDays  int `json:"inactive_days"`
	WithdrawnDays int `json:"withdrawn_days"`
}

func validateRetention(c retentionCfg) error {
	if c.InactiveDays != 0 && (c.InactiveDays < 30 || c.InactiveDays > 3650) {
		return errors.New("ระยะเวลาไม่มีความเคลื่อนไหว ต้องเป็น 0 (ปิด) หรือ 30 ถึง 3650 วัน")
	}
	if c.WithdrawnDays != 0 && (c.WithdrawnDays < 7 || c.WithdrawnDays > 3650) {
		return errors.New("ระยะเวลาหลังถอนความยินยอม ต้องเป็น 0 (ปิด) หรือ 7 ถึง 3650 วัน")
	}
	return nil
}

func initRetention(ctx context.Context, pool *pgxpool.Pool) error {
	for _, s := range []string{
		`CREATE TABLE IF NOT EXISTS app_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_by TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS retention_runs (
			id BIGSERIAL PRIMARY KEY, day DATE NOT NULL, ran_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			triggered_by TEXT NOT NULL, inactive_done INT NOT NULL DEFAULT 0, withdrawn_done INT NOT NULL DEFAULT 0)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS retention_runs_scheduled_day ON retention_runs(day) WHERE triggered_by='schedule'`,
	} {
		if _, err := pool.Exec(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

func loadRetention(ctx context.Context, pool *pgxpool.Pool) retentionCfg {
	cfg := retentionCfg{InactiveDays: envInt("RETENTION_INACTIVE_DAYS", 0), WithdrawnDays: envInt("RETENTION_WITHDRAWN_DAYS", 0)}
	rows, err := pool.Query(ctx, `SELECT key, value FROM app_settings WHERE key IN ('retention_inactive_days','retention_withdrawn_days')`)
	if err != nil {
		return cfg
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) != nil {
			continue
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			continue
		}
		if k == "retention_inactive_days" {
			cfg.InactiveDays = n
		} else {
			cfg.WithdrawnDays = n
		}
	}
	if validateRetention(cfg) != nil { // a bad stored value must never delete things: treat as off
		return retentionCfg{}
	}
	return cfg
}

// SQL for "which leads are due". Last activity = newest of: created, consent, an event that is not the daily job's
// own silence marker, a customer message. (The daily job writes events too; they must not keep a lead alive.)
const dueInactiveSQL = `
	SELECT l.id FROM leads l
	WHERE l.anonymized_at IS NULL AND l.status NOT IN ('qualified','handed_off')
	  AND GREATEST(l.created_at, l.consent_at,
	        (SELECT max(e.created_at) FROM lead_events e WHERE e.lead_id=l.id AND COALESCE(e.data->>'source','') <> 'daily_job'),
	        (SELECT max(m.created_at) FROM customer_messages m WHERE m.lead_id=l.id))
	      < now() - make_interval(days => $1)
	ORDER BY l.id LIMIT $2`

const dueWithdrawnSQL = `
	SELECT l.id FROM leads l
	WHERE l.anonymized_at IS NULL AND l.consent_withdrawn_at IS NOT NULL
	  AND l.consent_withdrawn_at < now() - make_interval(days => $1)
	ORDER BY l.id LIMIT $2`

func dueIDs(ctx context.Context, pool *pgxpool.Pool, sql string, days, limit int) ([]int64, error) {
	rows, err := pool.Query(ctx, sql, days, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// anonymizeLeadByID does what the admin's delete button does, for the retention job.
func anonymizeLeadByID(ctx context.Context, pool *pgxpool.Pool, id int64, reason string) (bool, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE leads SET display_name='(deleted)', name='', contact='', channel_user_id=NULL, budget=NULL,
		consent=FALSE, consent_general=FALSE, consent_marketing=FALSE,
		consent_withdrawn_at=COALESCE(consent_withdrawn_at, now()), anonymized_at=now(), updated_at=now()
		WHERE id=$1 AND anonymized_at IS NULL`, id)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	for _, q := range []string{`DELETE FROM lead_events WHERE lead_id=$1`, `DELETE FROM staff_replies WHERE lead_id=$1`, `DELETE FROM customer_messages WHERE lead_id=$1`} {
		if _, err := tx.Exec(ctx, q, id); err != nil {
			return false, err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_log (actor_id, actor_email, action, target, ip, detail) VALUES (NULL,'system:retention','lead_anonymized_retention',$1,'',$2)`,
		fmt.Sprintf("lead:%d", id), reason); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

type retentionResult struct {
	Inactive  int `json:"inactive_done"`
	Withdrawn int `json:"withdrawn_done"`
}

func runRetention(ctx context.Context, pool *pgxpool.Pool) (retentionResult, error) {
	var res retentionResult
	cfg := loadRetention(ctx, pool)
	if cfg.WithdrawnDays > 0 {
		ids, err := dueIDs(ctx, pool, dueWithdrawnSQL, cfg.WithdrawnDays, retentionBatch)
		if err != nil {
			return res, err
		}
		for _, id := range ids {
			if ok, err := anonymizeLeadByID(ctx, pool, id, fmt.Sprintf("consent withdrawn over %d days", cfg.WithdrawnDays)); err != nil {
				log.Printf("retention: lead %d: %v", id, err)
			} else if ok {
				res.Withdrawn++
			}
		}
	}
	if cfg.InactiveDays > 0 {
		ids, err := dueIDs(ctx, pool, dueInactiveSQL, cfg.InactiveDays, retentionBatch)
		if err != nil {
			return res, err
		}
		for _, id := range ids {
			if ok, err := anonymizeLeadByID(ctx, pool, id, fmt.Sprintf("no activity over %d days", cfg.InactiveDays)); err != nil {
				log.Printf("retention: lead %d: %v", id, err)
			} else if ok {
				res.Inactive++
			}
		}
	}
	return res, nil
}

// runRetentionScheduler: once a day after 03:00 Thailand time (a unique index makes two API copies run it once).
func runRetentionScheduler(ctx context.Context, pool *pgxpool.Pool) {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			day, due := bangkokDay(now, 3)
			if !due {
				continue
			}
			var id int64
			if err := pool.QueryRow(ctx, `INSERT INTO retention_runs(day, triggered_by) VALUES ($1::text::date,'schedule') ON CONFLICT DO NOTHING RETURNING id`, day).Scan(&id); err != nil {
				continue // already ran today (or db error: try again later)
			}
			res, err := runRetention(ctx, pool)
			if err != nil {
				log.Printf("retention job failed: %v", err)
				continue
			}
			_, _ = pool.Exec(ctx, `UPDATE retention_runs SET inactive_done=$2, withdrawn_done=$3 WHERE id=$1`, id, res.Inactive, res.Withdrawn)
			if res.Inactive+res.Withdrawn > 0 {
				log.Printf("retention job: anonymized %+v", res)
			}
		}
	}
}

func registerRetentionRoutes(app fiber.Router, pool *pgxpool.Pool) {
	admin := requireRole("admin")

	app.Get("/retention", admin, func(c *fiber.Ctx) error {
		ctx := c.Context()
		cfg := loadRetention(ctx, pool)
		out := fiber.Map{"inactive_days": cfg.InactiveDays, "withdrawn_days": cfg.WithdrawnDays, "inactive_due": 0, "withdrawn_due": 0}
		if cfg.InactiveDays > 0 {
			ids, err := dueIDs(ctx, pool, dueInactiveSQL, cfg.InactiveDays, 100000)
			if err != nil {
				return serverError(c, err)
			}
			out["inactive_due"] = len(ids)
		}
		if cfg.WithdrawnDays > 0 {
			ids, err := dueIDs(ctx, pool, dueWithdrawnSQL, cfg.WithdrawnDays, 100000)
			if err != nil {
				return serverError(c, err)
			}
			out["withdrawn_due"] = len(ids)
		}
		var ranAt *time.Time
		var by string
		var in, wd int
		if err := pool.QueryRow(ctx, `SELECT ran_at, triggered_by, inactive_done, withdrawn_done FROM retention_runs ORDER BY id DESC LIMIT 1`).Scan(&ranAt, &by, &in, &wd); err == nil {
			out["last_run"] = fiber.Map{"at": ranAt, "by": by, "inactive_done": in, "withdrawn_done": wd}
		}
		return c.JSON(out)
	})

	app.Put("/retention", admin, func(c *fiber.Ctx) error {
		var in retentionCfg
		if err := c.BodyParser(&in); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		if err := validateRetention(in); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		by := currentUser(c).Email
		for k, v := range map[string]int{"retention_inactive_days": in.InactiveDays, "retention_withdrawn_days": in.WithdrawnDays} {
			if _, err := pool.Exec(c.Context(), `INSERT INTO app_settings(key,value,updated_by) VALUES ($1,$2,$3)
				ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value, updated_at=now(), updated_by=EXCLUDED.updated_by`, k, strconv.Itoa(v), by); err != nil {
				return serverError(c, err)
			}
		}
		auditUser(c, pool, "retention_settings", "settings", fmt.Sprintf("inactive=%d withdrawn=%d", in.InactiveDays, in.WithdrawnDays))
		return c.JSON(in)
	})

	app.Post("/retention/run", admin, func(c *fiber.Ctx) error {
		res, err := runRetention(c.Context(), pool)
		if err != nil {
			return serverError(c, err)
		}
		_, _ = pool.Exec(c.Context(), `INSERT INTO retention_runs(day, triggered_by, inactive_done, withdrawn_done) VALUES (CURRENT_DATE, $1, $2, $3)`,
			"manual:"+currentUser(c).Email, res.Inactive, res.Withdrawn)
		auditUser(c, pool, "retention_run", "settings", fmt.Sprintf("inactive=%d withdrawn=%d", res.Inactive, res.Withdrawn))
		return c.JSON(res)
	})
}
