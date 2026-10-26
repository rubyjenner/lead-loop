package main

import (
	"context"
	"errors"
	"log"
	"sort"
	"sync"
	"time"
)

// Retry queue for AI work. When every AI provider failed and a template draft was saved instead, a job is
// queued so the draft can be upgraded to a real AI answer later. Only drafts that nobody has touched yet are
// replaced (see upgradeHandler). Standard library only, so it can be unit tested on its own.

type Job struct {
	Kind       string  `json:"kind"` // "plan" or "contents"
	CampaignID int     `json:"campaign_id"`
	ContentIDs []int64 `json:"content_ids,omitempty"`
	Channel    string  `json:"channel,omitempty"`
	UserEmail  string  `json:"user_email"`
	Attempts   int     `json:"attempts"`
}

// JobQueue is a delayed queue: a job becomes available at runAt.
type JobQueue interface {
	Push(ctx context.Context, j Job, runAt time.Time) error
	// PopReady removes and returns one job whose time has come, or nil when there is none.
	PopReady(ctx context.Context, now time.Time) (*Job, error)
}

// errPermanent marks a job that must not be retried (campaign deleted, quota used up, ...).
var errPermanent = errors.New("permanent job failure")

// jobBackoff is how long to wait before retry number attempt+1 (attempt starts at 0).
func jobBackoff(attempt int) time.Duration {
	steps := []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, 3 * time.Hour}
	if attempt < 0 {
		attempt = 0
	}
	if attempt >= len(steps) {
		return steps[len(steps)-1]
	}
	return steps[attempt]
}

// ---------- in-memory queue (tests, and fallback when Redis is not available) ----------

type memItem struct {
	at  time.Time
	job Job
}

type memQueue struct {
	mu    sync.Mutex
	items []memItem
}

func newMemQueue() *memQueue { return &memQueue{} }

func (q *memQueue) Push(_ context.Context, j Job, runAt time.Time) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = append(q.items, memItem{at: runAt, job: j})
	sort.SliceStable(q.items, func(a, b int) bool { return q.items[a].at.Before(q.items[b].at) })
	return nil
}

func (q *memQueue) PopReady(_ context.Context, now time.Time) (*Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 || q.items[0].at.After(now) {
		return nil, nil
	}
	j := q.items[0].job
	q.items = q.items[1:]
	return &j, nil
}

func (q *memQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// ---------- worker ----------

// processReady handles every job that is ready now. A failed job is queued again with a growing delay
// until maxAttempts is reached, then dropped (the template draft simply stays).
func processReady(ctx context.Context, q JobQueue, handle func(context.Context, Job) error, maxAttempts int) {
	for ctx.Err() == nil {
		j, err := q.PopReady(ctx, time.Now())
		if err != nil {
			log.Printf("ai job queue: %v", err)
			return
		}
		if j == nil {
			return
		}
		err = handle(ctx, *j)
		if err == nil {
			continue
		}
		if errors.Is(err, errPermanent) {
			log.Printf("ai job %s for campaign %d dropped: %v", j.Kind, j.CampaignID, err)
			continue
		}
		j.Attempts++
		if j.Attempts >= maxAttempts {
			log.Printf("ai job %s for campaign %d dropped after %d attempts", j.Kind, j.CampaignID, j.Attempts)
			continue
		}
		if perr := q.Push(ctx, *j, time.Now().Add(jobBackoff(j.Attempts))); perr != nil {
			log.Printf("ai job queue: cannot requeue: %v", perr)
		}
	}
}

// runWorker polls the queue until ctx is cancelled.
func runWorker(ctx context.Context, q JobQueue, handle func(context.Context, Job) error, poll time.Duration, maxAttempts int) {
	t := time.NewTicker(poll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			processReady(ctx, q, handle, maxAttempts)
		}
	}
}
