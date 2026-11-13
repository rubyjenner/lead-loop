package main

import (
	"bufio"
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

var notificationSubscribers = struct {
	sync.Mutex
	byUser map[string]map[chan struct{}]struct{}
}{byUser: map[string]map[chan struct{}]struct{}{}}

func subscribeNotifications(email string) (chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	key := lowerEmail(email)
	notificationSubscribers.Lock()
	if notificationSubscribers.byUser[key] == nil {
		notificationSubscribers.byUser[key] = map[chan struct{}]struct{}{}
	}
	notificationSubscribers.byUser[key][ch] = struct{}{}
	notificationSubscribers.Unlock()
	return ch, func() {
		notificationSubscribers.Lock()
		delete(notificationSubscribers.byUser[key], ch)
		if len(notificationSubscribers.byUser[key]) == 0 {
			delete(notificationSubscribers.byUser, key)
		}
		close(ch)
		notificationSubscribers.Unlock()
	}
}

func publishNotification(email string) {
	key := lowerEmail(email)
	notificationSubscribers.Lock()
	defer notificationSubscribers.Unlock()
	for ch := range notificationSubscribers.byUser[key] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// Notifications are private to each user. We store only workflow metadata, never customer chat text.
func initNotifications(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS user_notifications (
			id BIGSERIAL PRIMARY KEY,
			user_email TEXT NOT NULL,
			kind TEXT NOT NULL,
			title TEXT NOT NULL,
			body TEXT NOT NULL DEFAULT '',
			href TEXT NOT NULL DEFAULT '/overview',
			entity_key TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			read_at TIMESTAMPTZ
		);
		CREATE INDEX IF NOT EXISTS idx_user_notifications_user_created ON user_notifications(lower(user_email), created_at DESC);
		CREATE INDEX IF NOT EXISTS idx_user_notifications_unread ON user_notifications(lower(user_email)) WHERE read_at IS NULL;
	`)
	return err
}

func notifyUser(ctx context.Context, pool *pgxpool.Pool, email, kind, title, body, href, entity string) {
	if email == "" {
		return
	}
	tag, err := pool.Exec(ctx, `INSERT INTO user_notifications(user_email,kind,title,body,href,entity_key)
		VALUES($1,$2,$3,$4,$5,$6)`, email, kind, title, body, href, entity)
	if err == nil && tag.RowsAffected() > 0 {
		publishNotification(email)
	}
}

func notifyApprovers(ctx context.Context, pool *pgxpool.Pool, excludeEmails []string, kind, title, body, href, entity string) {
	rows, err := pool.Query(ctx, `SELECT email FROM users WHERE active AND role IN ('admin','marketing')`)
	if err != nil {
		return
	}
	defer rows.Close()
	blocked := map[string]bool{}
	for _, e := range excludeEmails {
		blocked[lowerEmail(e)] = true
	}
	for rows.Next() {
		var email string
		if rows.Scan(&email) == nil && !blocked[lowerEmail(email)] {
			notifyUser(ctx, pool, email, kind, title, body, href, entity)
		}
	}
}

func notifyRoles(ctx context.Context, pool *pgxpool.Pool, roles []string, kind, title, body, href, entity string) {
	rows, err := pool.Query(ctx, `SELECT email FROM users WHERE active AND role = ANY($1)`, roles)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var email string
		if rows.Scan(&email) == nil {
			notifyUser(ctx, pool, email, kind, title, body, href, entity)
		}
	}
}

func lowerEmail(s string) string { return strings.ToLower(s) }

func registerNotificationRoutes(app fiber.Router, pool *pgxpool.Pool) {
	app.Get("/notifications/stream", func(c *fiber.Ctx) error {
		u := currentUser(c)
		if u == nil {
			return c.SendStatus(401)
		}
		ch, unsubscribe := subscribeNotifications(u.Email)
		c.Set("Content-Type", "text/event-stream")
		c.Set("Cache-Control", "no-cache, no-transform")
		c.Set("Connection", "keep-alive")
		c.Set("X-Accel-Buffering", "no")
		c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
			defer unsubscribe()
			ticker := time.NewTicker(20 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ch:
					if _, err := fmt.Fprint(w, "event: notification\ndata: {}\n\n"); err != nil {
						return
					}
					if err := w.Flush(); err != nil {
						return
					}
				case <-ticker.C:
					if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
						return
					}
					if err := w.Flush(); err != nil {
						return
					}
				}
			}
		})
		return nil
	})
	app.Get("/notifications", func(c *fiber.Ctx) error {
		u := currentUser(c)
		if u == nil {
			return c.SendStatus(401)
		}
		limit := c.QueryInt("limit", 30)
		if limit < 1 || limit > 100 {
			limit = 30
		}
		var unread int
		if err := pool.QueryRow(c.Context(), `SELECT count(*) FROM user_notifications WHERE lower(user_email)=lower($1) AND read_at IS NULL`, u.Email).Scan(&unread); err != nil {
			return serverError(c, err)
		}
		rows, err := pool.Query(c.Context(), `SELECT id,kind,title,body,href,created_at,read_at FROM user_notifications
			WHERE lower(user_email)=lower($1) ORDER BY created_at DESC,id DESC LIMIT $2`, u.Email, limit)
		if err != nil {
			return serverError(c, err)
		}
		defer rows.Close()
		items := []fiber.Map{}
		for rows.Next() {
			var id int64
			var kind, title, body, href string
			var created time.Time
			var read *time.Time
			if err := rows.Scan(&id, &kind, &title, &body, &href, &created, &read); err != nil {
				return serverError(c, err)
			}
			items = append(items, fiber.Map{"id": id, "kind": kind, "title": title, "body": body, "href": href, "created_at": created, "read_at": read})
		}
		return c.JSON(fiber.Map{"unread_count": unread, "items": items})
	})
	app.Post("/notifications/:id/read", func(c *fiber.Ctx) error {
		u := currentUser(c)
		if u == nil {
			return c.SendStatus(401)
		}
		id, err := c.ParamsInt("id")
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid notification id"})
		}
		tag, err := pool.Exec(c.Context(), `UPDATE user_notifications SET read_at=COALESCE(read_at,now()) WHERE id=$1 AND lower(user_email)=lower($2)`, id, u.Email)
		if err != nil {
			return serverError(c, err)
		}
		if tag.RowsAffected() == 0 {
			return c.Status(404).JSON(fiber.Map{"error": "notification not found"})
		}
		return c.JSON(fiber.Map{"ok": true})
	})
	app.Post("/notifications/read-all", func(c *fiber.Ctx) error {
		u := currentUser(c)
		if u == nil {
			return c.SendStatus(401)
		}
		if _, err := pool.Exec(c.Context(), `UPDATE user_notifications SET read_at=now() WHERE lower(user_email)=lower($1) AND read_at IS NULL`, u.Email); err != nil {
			return serverError(c, err)
		}
		return c.JSON(fiber.Map{"ok": true})
	})
}
