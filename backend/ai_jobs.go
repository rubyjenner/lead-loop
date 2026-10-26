package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func envInt(name string, def int) int {
	if v := os.Getenv(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return def
}

// initQueue uses Redis when REDIS_URL is set and reachable; otherwise an in-memory queue
// (jobs are then lost on restart, which only means a template draft stays a template draft).
func initQueue(ctx context.Context) JobQueue {
	if u := os.Getenv("REDIS_URL"); u != "" {
		q, err := newRedisQueue(ctx, u)
		if err == nil {
			log.Printf("ai retry queue: redis")
			return q
		}
		log.Printf("redis unavailable (%v): using an in-memory retry queue", err)
	}
	return newMemQueue()
}

func initUsage(ctx context.Context, pool *pgxpool.Pool) error {
	for _, s := range []string{
		`CREATE TABLE IF NOT EXISTS ai_usage (
			id            BIGSERIAL PRIMARY KEY,
			at            TIMESTAMPTZ NOT NULL DEFAULT now(),
			user_email    TEXT NOT NULL DEFAULT '',
			campaign_id   BIGINT,
			action        TEXT NOT NULL,
			provider      TEXT NOT NULL,
			reason        TEXT NOT NULL DEFAULT '',
			input_tokens  INT NOT NULL DEFAULT 0,
			output_tokens INT NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_usage_user_at ON ai_usage(user_email, at)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_usage_at ON ai_usage(at)`,
	} {
		if _, err := pool.Exec(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

// recordUsage stores one AI request (including template fallbacks, with 0 tokens and the reason).
// It never stores prompts or answers. A failure to write is logged and does not fail the request.
func recordUsage(ctx context.Context, pool *pgxpool.Pool, email string, campaignID int, action string, r Result) {
	_, err := pool.Exec(ctx,
		`INSERT INTO ai_usage (user_email, campaign_id, action, provider, reason, input_tokens, output_tokens)
		 VALUES ($1, NULLIF($2::bigint, 0), $3, $4, $5, $6, $7)`,
		email, campaignID, action, r.By, r.Reason, r.In, r.Out)
	if err != nil {
		log.Printf("ai_usage write failed: %v", err)
	}
}

// start of today in Thailand time
const todayStart = `(date_trunc('day', now() AT TIME ZONE 'Asia/Bangkok') AT TIME ZONE 'Asia/Bangkok')`

// usageToday counts today's requests that were answered by an external provider.
func usageToday(ctx context.Context, pool *pgxpool.Pool, email string) (int, error) {
	var n int
	err := pool.QueryRow(ctx,
		`SELECT count(*) FROM ai_usage
		 WHERE user_email=$1 AND provider IN ('claude','gemini') AND at >= `+todayStart, email).Scan(&n)
	return n, err
}

// quotaExceeded checks the daily limits (AI_DAILY_QUOTA_PER_USER, default 30 external requests per user;
// AI_DAILY_TOKEN_CAP, default 0 = off, total tokens for everyone). 0 turns a limit off.
// When a limit is reached the request is still served, by the template algorithm.
func quotaExceeded(ctx context.Context, pool *pgxpool.Pool, email string) (bool, error) {
	if limit := envInt("AI_DAILY_QUOTA_PER_USER", 30); limit > 0 {
		n, err := usageToday(ctx, pool, email)
		if err != nil {
			return true, err
		}
		if n >= limit {
			return true, nil
		}
	}
	if cap := envInt("AI_DAILY_TOKEN_CAP", 0); cap > 0 {
		var tok int64
		err := pool.QueryRow(ctx,
			`SELECT coalesce(sum(input_tokens + output_tokens), 0) FROM ai_usage
			 WHERE provider IN ('claude','gemini') AND at >= `+todayStart).Scan(&tok)
		if err != nil {
			return true, err
		}
		if tok >= int64(cap) {
			return true, nil
		}
	}
	return false, nil
}

// upgradeHandler retries AI for a template draft. It only replaces text nobody has started working with:
// a plan that is still the template plan, or content that is still a 'draft' made by the template.
func upgradeHandler(pool *pgxpool.Pool) func(context.Context, Job) error {
	return func(ctx context.Context, j Job) error {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()

		over, err := quotaExceeded(ctx, pool, j.UserEmail)
		if err != nil {
			return err
		}
		if over {
			return fmt.Errorf("%w: daily quota reached", errPermanent)
		}
		ci, err := loadCampaign(ctx, pool, j.CampaignID)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: campaign no longer exists", errPermanent)
		}
		if err != nil {
			return err
		}

		switch j.Kind {
		case "plan":
			r := tryProviders(ctx, systemPrompt, planPrompt(ci))
			if r.By == "" {
				return errors.New("ai still unavailable")
			}
			recordUsage(ctx, pool, j.UserEmail, j.CampaignID, "retry_plan", r)
			_, err = pool.Exec(ctx,
				`UPDATE campaigns SET plan=$1, plan_generated_by=$2 WHERE id=$3 AND plan_generated_by='template'`,
				r.Text, r.By, j.CampaignID)
			return err
		case "contents":
			r := tryProviders(ctx, systemPrompt, contentsPrompt(ci, j.Channel, len(j.ContentIDs)))
			if r.By == "" {
				return errors.New("ai still unavailable")
			}
			recordUsage(ctx, pool, j.UserEmail, j.CampaignID, "retry_contents", r)
			for i, text := range splitDrafts(r.Text, len(j.ContentIDs)) {
				if _, err := pool.Exec(ctx,
					`UPDATE contents SET body=$1, generated_by=$2
					 WHERE id=$3 AND status='draft' AND generated_by='template'`,
					text, r.By, j.ContentIDs[i]); err != nil {
					return err
				}
			}
			return nil
		}
		return fmt.Errorf("%w: unknown job kind %q", errPermanent, j.Kind)
	}
}
