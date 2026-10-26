package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// A small Redis client (RESP protocol, standard library only) and a delayed job queue on a sorted set.
// Throughput needs are tiny (a few retry jobs), so one connection guarded by a mutex is enough; it
// reconnects after an error. Jobs are stored as JSON members scored by their run time.

const queueKey = "leadloop:ai_jobs"

type respClient struct {
	mu       sync.Mutex
	addr     string
	password string
	db       int
	conn     net.Conn
	rd       *bufio.Reader
}

// parseRedisURL accepts redis://[:password@]host:port[/db]
func parseRedisURL(raw string) (addr, password string, db int, err error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "redis" || u.Host == "" {
		return "", "", 0, errors.New("invalid REDIS_URL (expected redis://host:port)")
	}
	addr = u.Host
	if u.Port() == "" {
		addr = net.JoinHostPort(u.Hostname(), "6379")
	}
	if u.User != nil {
		password, _ = u.User.Password()
	}
	if p := strings.Trim(u.Path, "/"); p != "" {
		if db, err = strconv.Atoi(p); err != nil {
			return "", "", 0, errors.New("invalid database number in REDIS_URL")
		}
	}
	return addr, password, db, nil
}

func newRespClient(raw string) (*respClient, error) {
	addr, pw, db, err := parseRedisURL(raw)
	if err != nil {
		return nil, err
	}
	return &respClient{addr: addr, password: pw, db: db}, nil
}

func (c *respClient) connect(ctx context.Context) error {
	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", c.addr)
	if err != nil {
		return err
	}
	c.conn, c.rd = conn, bufio.NewReader(conn)
	if c.password != "" {
		if _, err := c.roundTrip("AUTH", c.password); err != nil {
			c.close()
			return err
		}
	}
	if c.db != 0 {
		if _, err := c.roundTrip("SELECT", strconv.Itoa(c.db)); err != nil {
			c.close()
			return err
		}
	}
	return nil
}

func (c *respClient) close() {
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.conn, c.rd = nil, nil
}

// do runs one command, connecting first if needed. On a network error the connection is dropped so the
// next call reconnects. An error reply from Redis (for example WRONGTYPE) does not drop the connection.
func (c *respClient) do(ctx context.Context, args ...string) (any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		if err := c.connect(ctx); err != nil {
			return nil, err
		}
	}
	v, err := c.roundTrip(args...)
	var re redisError
	if err != nil && !errors.As(err, &re) {
		c.close()
	}
	return v, err
}

type redisError string

func (e redisError) Error() string { return "redis: " + string(e) }

func (c *respClient) roundTrip(args ...string) (any, error) {
	_ = c.conn.SetDeadline(time.Now().Add(5 * time.Second))
	var b strings.Builder
	fmt.Fprintf(&b, "*%d\r\n", len(args))
	for _, a := range args {
		fmt.Fprintf(&b, "$%d\r\n%s\r\n", len(a), a)
	}
	if _, err := io.WriteString(c.conn, b.String()); err != nil {
		return nil, err
	}
	return readReply(c.rd)
}

func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func readReply(r *bufio.Reader) (any, error) {
	line, err := readLine(r)
	if err != nil {
		return nil, err
	}
	if line == "" {
		return nil, errors.New("redis: empty reply")
	}
	body := line[1:]
	switch line[0] {
	case '+':
		return body, nil
	case '-':
		return nil, redisError(body)
	case ':':
		n, err := strconv.ParseInt(body, 10, 64)
		return n, err
	case '$':
		n, err := strconv.Atoi(body)
		if err != nil {
			return nil, err
		}
		if n < 0 {
			return nil, nil
		}
		buf := make([]byte, n+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		return string(buf[:n]), nil
	case '*':
		n, err := strconv.Atoi(body)
		if err != nil {
			return nil, err
		}
		if n < 0 {
			return nil, nil
		}
		out := make([]any, n)
		for i := range out {
			if out[i], err = readReply(r); err != nil {
				return nil, err
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("redis: unexpected reply %q", line)
}

// ---------- delayed job queue ----------

type redisQueue struct{ c *respClient }

// newRedisQueue connects and checks the server with PING.
func newRedisQueue(ctx context.Context, rawURL string) (*redisQueue, error) {
	c, err := newRespClient(rawURL)
	if err != nil {
		return nil, err
	}
	if _, err := c.do(ctx, "PING"); err != nil {
		return nil, err
	}
	return &redisQueue{c: c}, nil
}

func (q *redisQueue) Push(ctx context.Context, j Job, runAt time.Time) error {
	b, err := json.Marshal(j)
	if err != nil {
		return err
	}
	_, err = q.c.do(ctx, "ZADD", queueKey, strconv.FormatInt(runAt.Unix(), 10), string(b))
	return err
}

func (q *redisQueue) PopReady(ctx context.Context, now time.Time) (*Job, error) {
	v, err := q.c.do(ctx, "ZRANGEBYSCORE", queueKey, "-inf", strconv.FormatInt(now.Unix(), 10), "LIMIT", "0", "1")
	if err != nil {
		return nil, err
	}
	items, _ := v.([]any)
	if len(items) == 0 {
		return nil, nil
	}
	member, _ := items[0].(string)
	// ZREM returns 1 only for the caller that actually removed it, so two workers never run the same job
	n, err := q.c.do(ctx, "ZREM", queueKey, member)
	if err != nil {
		return nil, err
	}
	if removed, _ := n.(int64); removed == 0 {
		return nil, nil
	}
	var j Job
	if err := json.Unmarshal([]byte(member), &j); err != nil {
		return nil, fmt.Errorf("bad job in queue: %w", err)
	}
	return &j, nil
}
