package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/mail"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

const (
	inviteTTL  = 72 * time.Hour // invite link for a new account
	resetTTL   = 1 * time.Hour  // forgot-password link
	maxNameLen = 100
	// roles that can approve content (same roles as the "writers" group in main.go)
	minApprovers = 2
)

// randomString returns n random bytes encoded as URL-safe base64 (no padding).
func randomString(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error()) // never happens on a healthy system
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}

// execer is satisfied by both *pgxpool.Pool and pgx.Tx.
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// createUserToken revokes the user's older unused links and stores a new one (hash only).
// The raw token is returned once and never stored.
func createUserToken(ctx context.Context, q execer, userID int64, purpose string, ttl time.Duration, createdBy int64) (string, time.Time, error) {
	tok := randomString(32)
	exp := time.Now().Add(ttl)
	if _, err := q.Exec(ctx,
		`UPDATE user_tokens SET used_at=now() WHERE user_id=$1 AND used_at IS NULL`, userID); err != nil {
		return "", time.Time{}, err
	}
	if _, err := q.Exec(ctx,
		`INSERT INTO user_tokens (user_id, purpose, token_hash, expires_at, created_by)
		 VALUES ($1,$2,$3,$4,NULLIF($5::bigint,0))`, userID, purpose, hashToken(tok), exp, createdBy); err != nil {
		return "", time.Time{}, err
	}
	return tok, exp, nil
}

// linkFor builds the link the admin copies. APP_BASE_URL is the address of the web app
// (e.g. https://leadloop.example.com); when empty only the relative path is returned.
func linkFor(tok string) string {
	return strings.TrimRight(os.Getenv("APP_BASE_URL"), "/") + "/set-password?token=" + tok
}

func validEmail(e string) bool {
	if e == "" || len(e) > 254 || strings.ContainsAny(e, " \t\r\n<>,;") {
		return false
	}
	a, err := mail.ParseAddress(e)
	if err != nil || a.Address != e {
		return false
	}
	at := strings.LastIndex(e, "@")
	return at > 0 && strings.Contains(e[at+1:], ".")
}

// emailDomainAllowed implements the optional domain restriction. ALLOWED_EMAIL_DOMAINS is a
// comma-separated list (e.g. "vannessplus.com,example.com"); empty means no restriction.
func emailDomainAllowed(email string) bool {
	return domainAllowed(email, os.Getenv("ALLOWED_EMAIL_DOMAINS"))
}

func domainAllowed(email, list string) bool {
	if strings.TrimSpace(list) == "" {
		return true
	}
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return false
	}
	domain := strings.ToLower(email[at+1:])
	for _, d := range strings.Split(list, ",") {
		d = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(d), "@")))
		if d != "" && d == domain {
			return true
		}
	}
	return false
}

// registerSetPasswordRoute is public: the one-time link itself is the credential.
// Failed attempts are limited per IP so tokens cannot be guessed (they are 256-bit anyway).
func registerSetPasswordRoute(app *fiber.App, pool *pgxpool.Pool, tooMany fiber.Handler) {
	app.Post("/auth/set-password", limiter.New(limiter.Config{
		Max:                    loginMaxAttempts,
		Expiration:             loginWindow,
		SkipSuccessfulRequests: true,
		LimitReached:           tooMany,
	}), func(c *fiber.Ctx) error {
		var in struct {
			Token       string `json:"token"`
			NewPassword string `json:"new_password"`
		}
		if err := c.BodyParser(&in); err != nil || in.Token == "" {
			return c.Status(400).JSON(fiber.Map{"error": "token and new_password are required"})
		}
		if len(in.NewPassword) < minPasswordLen || len(in.NewPassword) > maxPasswordLen {
			return c.Status(400).JSON(fiber.Map{"error": "new password must be 8-72 characters"})
		}
		invalid := func() error {
			return c.Status(400).JSON(fiber.Map{"error": "link is invalid, expired or already used"})
		}

		ctx := c.UserContext()
		tx, err := pool.Begin(ctx)
		if err != nil {
			return serverError(c, err)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		var (
			tokID, userID int64
			purpose, prov string
			email         string
			active        bool
		)
		err = tx.QueryRow(ctx,
			`SELECT t.id, t.user_id, t.purpose, u.email, u.active, u.auth_provider
			 FROM user_tokens t JOIN users u ON u.id = t.user_id
			 WHERE t.token_hash=$1 AND t.used_at IS NULL AND t.expires_at > now()
			 FOR UPDATE OF t`, hashToken(in.Token)).Scan(&tokID, &userID, &purpose, &email, &active, &prov)
		if errors.Is(err, pgx.ErrNoRows) {
			audit(c, pool, 0, "", "set_password_failed", "", "invalid or expired link")
			return invalid()
		}
		if err != nil {
			return serverError(c, err)
		}
		if !active || prov != "local" {
			audit(c, pool, userID, email, "set_password_failed", "user:"+strconv.FormatInt(userID, 10), "account not eligible")
			return invalid()
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(in.NewPassword), bcrypt.DefaultCost)
		if err != nil {
			return serverError(c, err)
		}
		// new password chosen by the user: no forced change, old sessions stop working
		if _, err = tx.Exec(ctx,
			`UPDATE users SET password_hash=$1, must_change_password=false, token_version=token_version+1
			 WHERE id=$2`, string(hash), userID); err != nil {
			return serverError(c, err)
		}
		if _, err = tx.Exec(ctx,
			`UPDATE user_tokens SET used_at=now() WHERE user_id=$1 AND used_at IS NULL`, userID); err != nil {
			return serverError(c, err)
		}
		if err = tx.Commit(ctx); err != nil {
			return serverError(c, err)
		}
		audit(c, pool, userID, email, "password_set_via_link", "user:"+strconv.FormatInt(userID, 10), purpose)
		return c.JSON(fiber.Map{"status": "password set, you can log in now"})
	})
}

// registerUserAdminRoutes: admin-only helpers for the Manage Users page.
func registerUserAdminRoutes(app *fiber.App, pool *pgxpool.Pool, admin fiber.Handler) {
	// Forgot password / re-invite: creates a one-time link (valid 1 h) for the admin to copy and send.
	// The current password keeps working until the link is used.
	app.Post("/users/:id/reset-link", admin, func(c *fiber.Ctx) error {
		id, err := c.ParamsInt("id")
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid user id"})
		}
		var email, prov string
		var active bool
		err = pool.QueryRow(c.UserContext(),
			`SELECT email, active, auth_provider FROM users WHERE id=$1`, id).Scan(&email, &active, &prov)
		if errors.Is(err, pgx.ErrNoRows) {
			return c.Status(404).JSON(fiber.Map{"error": "user not found"})
		}
		if err != nil {
			return serverError(c, err)
		}
		if !active {
			return c.Status(409).JSON(fiber.Map{"error": "user is disabled"})
		}
		if prov != "local" {
			return c.Status(409).JSON(fiber.Map{"error": "user does not log in with a password"})
		}
		tok, exp, err := createUserToken(c.UserContext(), pool, int64(id), "reset", resetTTL, currentUserID(c))
		if err != nil {
			return serverError(c, err)
		}
		auditUser(c, pool, "reset_link_created", "user:"+strconv.Itoa(id), email)
		return c.Status(201).JSON(fiber.Map{"reset_url": linkFor(tok), "expires_at": exp})
	})

	// Warns when fewer than 2 active people can approve content (the approver must differ from the creator).
	app.Get("/users/approver-check", admin, func(c *fiber.Ctx) error {
		var n int
		if err := pool.QueryRow(c.UserContext(),
			`SELECT count(*) FROM users WHERE active AND role IN ('marketing','admin')`).Scan(&n); err != nil {
			return serverError(c, err)
		}
		out := fiber.Map{"active_approvers": n, "minimum": minApprovers, "ok": n >= minApprovers}
		if n < minApprovers {
			out["warning"] = "fewer than 2 active marketing/admin users: content cannot be approved by someone other than its creator"
		}
		return c.JSON(out)
	})
}
