package main

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

func TestJobBackoffGrowsAndCaps(t *testing.T) {
	if jobBackoff(0) != time.Minute || jobBackoff(1) != 5*time.Minute {
		t.Errorf("unexpected first steps")
	}
	if jobBackoff(99) != 3*time.Hour || jobBackoff(-1) != time.Minute {
		t.Errorf("cap or negative input wrong")
	}
}

func TestMemQueueRespectsRunTimeAndOrder(t *testing.T) {
	q, ctx, now := newMemQueue(), context.Background(), time.Now()
	_ = q.Push(ctx, Job{Kind: "later"}, now.Add(time.Hour))
	_ = q.Push(ctx, Job{Kind: "second"}, now.Add(-time.Second))
	_ = q.Push(ctx, Job{Kind: "first"}, now.Add(-time.Minute))
	for _, want := range []string{"first", "second"} {
		j, _ := q.PopReady(ctx, now)
		if j == nil || j.Kind != want {
			t.Fatalf("want %s, got %+v", want, j)
		}
	}
	if j, _ := q.PopReady(ctx, now); j != nil {
		t.Errorf("a future job must not be returned: %+v", j)
	}
}

func TestWorkerRetriesThenSucceeds(t *testing.T) {
	q, ctx := newMemQueue(), context.Background()
	_ = q.Push(ctx, Job{Kind: "plan", CampaignID: 1}, time.Now())
	calls := 0
	processReady(ctx, q, func(context.Context, Job) error {
		calls++
		return errors.New("ai still down")
	}, 3)
	if calls != 1 || q.Len() != 1 {
		t.Fatalf("a failed job must be queued again for later: calls=%d queued=%d", calls, q.Len())
	}
	j, _ := q.PopReady(ctx, time.Now().Add(10*time.Minute))
	if j == nil || j.Attempts != 1 {
		t.Fatalf("job should be ready after its backoff with 1 attempt, got %+v", j)
	}
}

func TestWorkerDropsAfterMaxAttemptsAndOnPermanentError(t *testing.T) {
	ctx := context.Background()
	q := newMemQueue()
	_ = q.Push(ctx, Job{Kind: "plan", Attempts: 2}, time.Now())
	processReady(ctx, q, func(context.Context, Job) error { return errors.New("down") }, 3)
	if q.Len() != 0 {
		t.Errorf("job should be dropped at max attempts")
	}
	_ = q.Push(ctx, Job{Kind: "plan"}, time.Now())
	processReady(ctx, q, func(context.Context, Job) error { return errPermanent }, 5)
	if q.Len() != 0 {
		t.Errorf("permanent failures must not be retried")
	}
}

func TestParseRedisURL(t *testing.T) {
	addr, pw, db, err := parseRedisURL("redis://:secret@cache:6380/2")
	if err != nil || addr != "cache:6380" || pw != "secret" || db != 2 {
		t.Errorf("got %q %q %d %v", addr, pw, db, err)
	}
	if a, _, _, err := parseRedisURL("redis://redis"); err != nil || a != "redis:6379" {
		t.Errorf("default port: %q %v", a, err)
	}
	if _, _, _, err := parseRedisURL("http://x"); err == nil {
		t.Errorf("wrong scheme must fail")
	}
}

// Runs against a real redis-server when one is installed (skipped otherwise, e.g. in the Go docker image).
func TestRedisQueueAgainstRealServer(t *testing.T) {
	bin, err := exec.LookPath("redis-server")
	if err != nil {
		t.Skip("redis-server not installed")
	}
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	cmd := exec.Command(bin, "--port", itoa(port), "--save", "", "--appendonly", "no", "--bind", "127.0.0.1")
	cmd.Stdout, cmd.Stderr = nil, nil
	if err := cmd.Start(); err != nil {
		t.Skip("cannot start redis-server")
	}
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }()

	ctx := context.Background()
	var q *redisQueue
	for i := 0; i < 50; i++ {
		if q, err = newRedisQueue(ctx, "redis://127.0.0.1:"+itoa(port)); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("cannot connect: %v", err)
	}
	now := time.Now()
	_ = q.Push(ctx, Job{Kind: "contents", CampaignID: 7, ContentIDs: []int64{1, 2}, UserEmail: "a@b.c"}, now.Add(-time.Second))
	_ = q.Push(ctx, Job{Kind: "plan", CampaignID: 8}, now.Add(time.Hour))
	j, err := q.PopReady(ctx, now)
	if err != nil || j == nil || j.CampaignID != 7 || len(j.ContentIDs) != 2 || j.UserEmail != "a@b.c" {
		t.Fatalf("pop: %+v %v", j, err)
	}
	if j, _ := q.PopReady(ctx, now); j != nil {
		t.Errorf("future job returned: %+v", j)
	}
	if j, _ := q.PopReady(ctx, now.Add(2*time.Hour)); j == nil || j.Kind != "plan" {
		t.Errorf("job should be ready later: %+v", j)
	}
	if j, _ := q.PopReady(ctx, now.Add(2*time.Hour)); j != nil {
		t.Errorf("queue should be empty")
	}
	// a Redis error reply must not break the client
	if _, err := q.c.do(ctx, "NOSUCHCOMMAND"); err == nil {
		t.Errorf("expected an error reply")
	}
	if _, err := q.c.do(ctx, "PING"); err != nil {
		t.Errorf("client should still work after an error reply: %v", err)
	}
	_ = os.Stdout
}

func itoa(n int) string { return strconv.Itoa(n) }
