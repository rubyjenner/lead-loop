package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

const (
	tokenTTL         = 8 * time.Hour
	loginMaxAttempts = 5
	loginWindow      = 15 * time.Minute
	minPasswordLen   = 8
	maxPasswordLen   = 72 // bcrypt limit
)

var (
	jwtSecret  []byte
	dummyHash  []byte // compared against when the email is unknown, so timing does not reveal valid emails
	validRoles = map[string]bool{"admin": true, "marketing": true, "sales": true}
)

type Claims struct {
	Email        string `json:"email"`
	Role         string `json:"role"`
	TokenVersion int    `json:"tv"` // bumped on password change/reset to invalidate old tokens
	jwt.RegisteredClaims
}

// initAuth checks the secret, creates/upgrades the users and audit_log tables and seeds the first admin.
func initAuth(ctx context.Context, pool *pgxpool.Pool) error {
	secret := os.Getenv("JWT_SECRET")
	if len(secret) < 32 {
		return errors.New("JWT_SECRET must be set and at least 32 characters")
	}
	jwtSecret = []byte(secret)

	var err error
	if dummyHash, err = bcrypt.GenerateFromPassword([]byte("dummy-password"), bcrypt.DefaultCost); err != nil {
		return err
	}

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id            BIGSERIAL PRIMARY KEY,
			email         TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			role          TEXT NOT NULL CHECK (role IN ('admin','marketing','sales')),
			created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS active BOOLEAN NOT NULL DEFAULT true`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS must_change_password BOOLEAN NOT NULL DEFAULT false`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS token_version INT NOT NULL DEFAULT 0`,
		`CREATE TABLE IF NOT EXISTS audit_log (
			id          BIGSERIAL PRIMARY KEY,
			at          TIMESTAMPTZ NOT NULL DEFAULT now(),
			actor_id    BIGINT,
			actor_email TEXT NOT NULL DEFAULT '',
			action      TEXT NOT NULL,
			target      TEXT NOT NULL DEFAULT '',
			ip          TEXT NOT NULL DEFAULT '',
			detail      TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_log_at ON audit_log(at)`,
	}
	for _, s := range stmts {
		if _, err = pool.Exec(ctx, s); err != nil {
			return err
		}
	}

	var n int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	email := strings.ToLower(strings.TrimSpace(os.Getenv("ADMIN_EMAIL")))
	password := os.Getenv("ADMIN_PASSWORD")
	if email == "" || len(password) < minPasswordLen || len(password) > maxPasswordLen {
		return errors.New("no users yet: set ADMIN_EMAIL and ADMIN_PASSWORD (8-72 characters) to create the first admin")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if _, err = pool.Exec(ctx,
		`INSERT INTO users (email, password_hash, role) VALUES ($1,$2,'admin')`, email, string(hash)); err != nil {
		return err
	}
	log.Printf("created first admin user %s", email)
	return nil
}

func issueToken(id int64, email, role string, tv int) (string, error) {
	now := time.Now()
	claims := Claims{
		Email:        email,
		Role:         role,
		TokenVersion: tv,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatInt(id, 10),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(tokenTTL)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(jwtSecret)
}

func unauthorized(c *fiber.Ctx) error {
	return c.Status(401).JSON(fiber.Map{"error": "unauthorized"})
}

// requireAuth accepts "Authorization: Bearer <token>", then re-checks the user in the database on every
// request: the account must still exist and be active, the token version must match, and the role used
// is the CURRENT role from the database (not the one stored in the token). Users who must change their
// password can only reach /auth/me and /auth/password.
func requireAuth(pool *pgxpool.Pool) fiber.Handler {
	return func(c *fiber.Ctx) error {
		h := c.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			return unauthorized(c)
		}
		claims := &Claims{}
		tok, err := jwt.ParseWithClaims(strings.TrimPrefix(h, "Bearer "), claims,
			func(*jwt.Token) (any, error) { return jwtSecret, nil },
			jwt.WithValidMethods([]string{"HS256"}))
		if err != nil || !tok.Valid {
			return unauthorized(c)
		}
		id, err := strconv.ParseInt(claims.Subject, 10, 64)
		if err != nil {
			return unauthorized(c)
		}

		var (
			email, role string
			active      bool
			mustChange  bool
			tv          int
		)
		err = pool.QueryRow(c.UserContext(),
			`SELECT email, role, active, must_change_password, token_version FROM users WHERE id=$1`, id,
		).Scan(&email, &role, &active, &mustChange, &tv)
		if errors.Is(err, pgx.ErrNoRows) {
			return unauthorized(c)
		}
		if err != nil {
			return serverError(c, err)
		}
		if !active || tv != claims.TokenVersion {
			return unauthorized(c)
		}

		claims.Email = email
		claims.Role = role
		c.Locals("user", claims)

		if mustChange && c.Path() != "/auth/me" && c.Path() != "/auth/password" {
			return c.Status(403).JSON(fiber.Map{
				"error": "password change required",
				"code":  "password_change_required",
			})
		}
		return c.Next()
	}
}

func currentUser(c *fiber.Ctx) *Claims {
	u, _ := c.Locals("user").(*Claims)
	return u
}

func currentUserID(c *fiber.Ctx) int64 {
	u := currentUser(c)
	if u == nil {
		return 0
	}
	id, _ := strconv.ParseInt(u.Subject, 10, 64)
	return id
}

func requireRole(roles ...string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if u := currentUser(c); u != nil {
			for _, r := range roles {
				if u.Role == r {
					return c.Next()
				}
			}
		}
		return c.Status(403).JSON(fiber.Map{"error": "forbidden for your role"})
	}
}

// ---------- audit log ----------

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// audit records who did what. It never stores passwords or tokens, and a failure to write the log
// is logged on the server but does not fail the request.
func audit(c *fiber.Ctx, pool *pgxpool.Pool, actorID int64, actorEmail, action, target, detail string) {
	_, err := pool.Exec(c.UserContext(),
		`INSERT INTO audit_log (actor_id, actor_email, action, target, ip, detail)
		 VALUES (NULLIF($1::bigint, 0), $2, $3, $4, $5, $6)`,
		actorID, trunc(actorEmail, 254), action, trunc(target, 254), c.IP(), trunc(detail, 500))
	if err != nil {
		log.Printf("audit log write failed (%s): %v", action, err)
	}
}

// auditUser records an action by the logged-in user.
func auditUser(c *fiber.Ctx, pool *pgxpool.Pool, action, target, detail string) {
	email := ""
	if u := currentUser(c); u != nil {
		email = u.Email
	}
	audit(c, pool, currentUserID(c), email, action, target, detail)
}

// registerAuthRoutes registers the public login route, then requires auth for everything
// registered after it (Fiber runs handlers in registration order).
func registerAuthRoutes(app *fiber.App, pool *pgxpool.Pool) {
	tooMany := func(c *fiber.Ctx) error {
		return c.Status(429).JSON(fiber.Map{"error": "too many failed attempts, try again in 15 minutes"})
	}

	// At most 5 failed logins per IP per 15 minutes. Successful logins are not counted.
	app.Post("/auth/login", limiter.New(limiter.Config{
		Max:                    loginMaxAttempts,
		Expiration:             loginWindow,
		SkipSuccessfulRequests: true,
		LimitReached:           tooMany,
	}), func(c *fiber.Ctx) error {
		var in struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := c.BodyParser(&in); err != nil || in.Email == "" || in.Password == "" {
			return c.Status(400).JSON(fiber.Map{"error": "email and password are required"})
		}
		email := strings.ToLower(strings.TrimSpace(in.Email))

		var (
			id         int64
			hash, role string
			active     bool
			mustChange bool
			tv         int
		)
		err := pool.QueryRow(c.UserContext(),
			`SELECT id, password_hash, role, active, must_change_password, token_version
			 FROM users WHERE email=$1`, email).Scan(&id, &hash, &role, &active, &mustChange, &tv)
		if errors.Is(err, pgx.ErrNoRows) {
			_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(in.Password))
			audit(c, pool, 0, email, "login_failed", email, "unknown email")
			return c.Status(401).JSON(fiber.Map{"error": "invalid email or password"})
		}
		if err != nil {
			return serverError(c, err)
		}
		// Same error and same work whether the password is wrong or the account is disabled.
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(in.Password)) != nil || !active {
			audit(c, pool, id, email, "login_failed", email, "")
			return c.Status(401).JSON(fiber.Map{"error": "invalid email or password"})
		}
		tok, err := issueToken(id, email, role, tv)
		if err != nil {
			return serverError(c, err)
		}
		audit(c, pool, id, email, "login_success", email, "")
		return c.JSON(fiber.Map{
			"token":                tok,
			"role":                 role,
			"expires_in":           int(tokenTTL.Seconds()),
			"must_change_password": mustChange,
		})
	})

	// everything below requires a valid, active user
	app.Use(requireAuth(pool))

	app.Get("/auth/me", func(c *fiber.Ctx) error {
		u := currentUser(c)
		return c.JSON(fiber.Map{"id": u.Subject, "email": u.Email, "role": u.Role})
	})

	// Change own password. Needs the current password (rate limited per user), then returns a new
	// token; all older tokens of this user stop working.
	app.Post("/auth/password", limiter.New(limiter.Config{
		Max:                    loginMaxAttempts,
		Expiration:             loginWindow,
		SkipSuccessfulRequests: true,
		KeyGenerator:           func(c *fiber.Ctx) string { return "pw:" + currentUser(c).Subject },
		LimitReached:           tooMany,
	}), func(c *fiber.Ctx) error {
		var in struct {
			CurrentPassword string `json:"current_password"`
			NewPassword     string `json:"new_password"`
		}
		if err := c.BodyParser(&in); err != nil || in.CurrentPassword == "" {
			return c.Status(400).JSON(fiber.Map{"error": "current_password and new_password are required"})
		}
		if len(in.NewPassword) < minPasswordLen || len(in.NewPassword) > maxPasswordLen {
			return c.Status(400).JSON(fiber.Map{"error": "new password must be 8-72 characters"})
		}
		if in.NewPassword == in.CurrentPassword {
			return c.Status(400).JSON(fiber.Map{"error": "new password must be different from the current one"})
		}

		id := currentUserID(c)
		var hash string
		if err := pool.QueryRow(c.UserContext(), `SELECT password_hash FROM users WHERE id=$1`, id).Scan(&hash); err != nil {
			return serverError(c, err)
		}
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(in.CurrentPassword)) != nil {
			auditUser(c, pool, "password_change_failed", "user:"+strconv.FormatInt(id, 10), "wrong current password")
			return c.Status(400).JSON(fiber.Map{"error": "current password is incorrect"})
		}
		newHash, err := bcrypt.GenerateFromPassword([]byte(in.NewPassword), bcrypt.DefaultCost)
		if err != nil {
			return serverError(c, err)
		}
		var newTV int
		err = pool.QueryRow(c.UserContext(),
			`UPDATE users SET password_hash=$1, must_change_password=false, token_version=token_version+1
			 WHERE id=$2 RETURNING token_version`, string(newHash), id).Scan(&newTV)
		if err != nil {
			return serverError(c, err)
		}
		u := currentUser(c)
		tok, err := issueToken(id, u.Email, u.Role, newTV)
		if err != nil {
			return serverError(c, err)
		}
		auditUser(c, pool, "password_changed", "user:"+strconv.FormatInt(id, 10), "")
		return c.JSON(fiber.Map{"token": tok, "expires_in": int(tokenTTL.Seconds())})
	})

	admin := requireRole("admin")

	// Create a user. They must change the password at first login.
	app.Post("/users", admin, func(c *fiber.Ctx) error {
		var in struct {
			Email    string `json:"email"`
			Password string `json:"password"`
			Role     string `json:"role"`
		}
		if err := c.BodyParser(&in); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		in.Email = strings.ToLower(strings.TrimSpace(in.Email))
		if in.Email == "" || !strings.Contains(in.Email, "@") {
			return c.Status(400).JSON(fiber.Map{"error": "valid email is required"})
		}
		if len(in.Password) < minPasswordLen || len(in.Password) > maxPasswordLen {
			return c.Status(400).JSON(fiber.Map{"error": "password must be 8-72 characters"})
		}
		if !validRoles[in.Role] {
			return c.Status(400).JSON(fiber.Map{"error": "role must be admin, marketing or sales"})
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
		if err != nil {
			return serverError(c, err)
		}
		var id int64
		err = pool.QueryRow(c.UserContext(),
			`INSERT INTO users (email, password_hash, role, must_change_password)
			 VALUES ($1,$2,$3,true) RETURNING id`,
			in.Email, string(hash), in.Role).Scan(&id)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return c.Status(409).JSON(fiber.Map{"error": "email already exists"})
		}
		if err != nil {
			return serverError(c, err)
		}
		auditUser(c, pool, "user_created", "user:"+strconv.FormatInt(id, 10), in.Email+" role="+in.Role)
		return c.Status(201).JSON(fiber.Map{"id": id, "email": in.Email, "role": in.Role, "must_change_password": true})
	})

	app.Get("/users", admin, func(c *fiber.Ctx) error {
		rows, err := pool.Query(c.UserContext(),
			`SELECT id, email, role, active, must_change_password, created_at FROM users ORDER BY id`)
		if err != nil {
			return serverError(c, err)
		}
		defer rows.Close()
		type user struct {
			ID                 int64     `json:"id"`
			Email              string    `json:"email"`
			Role               string    `json:"role"`
			Active             bool      `json:"active"`
			MustChangePassword bool      `json:"must_change_password"`
			CreatedAt          time.Time `json:"created_at"`
		}
		out := []user{}
		for rows.Next() {
			var u user
			if err := rows.Scan(&u.ID, &u.Email, &u.Role, &u.Active, &u.MustChangePassword, &u.CreatedAt); err != nil {
				return serverError(c, err)
			}
			out = append(out, u)
		}
		return c.JSON(out)
	})

	// Change role and/or enable/disable a user. A disabled user is rejected on their very next request.
	// The last active admin can never be demoted or disabled.
	app.Patch("/users/:id", admin, func(c *fiber.Ctx) error {
		id, err := c.ParamsInt("id")
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid user id"})
		}
		var in struct {
			Role   *string `json:"role"`
			Active *bool   `json:"active"`
		}
		if err := c.BodyParser(&in); err != nil || (in.Role == nil && in.Active == nil) {
			return c.Status(400).JSON(fiber.Map{"error": "provide role and/or active"})
		}
		if in.Role != nil && !validRoles[*in.Role] {
			return c.Status(400).JSON(fiber.Map{"error": "role must be admin, marketing or sales"})
		}

		ctx := c.UserContext()
		tx, err := pool.Begin(ctx)
		if err != nil {
			return serverError(c, err)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		// serialize admin changes so two admins cannot remove each other at the same time
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7001)`); err != nil {
			return serverError(c, err)
		}
		var curRole string
		var curActive bool
		err = tx.QueryRow(ctx, `SELECT role, active FROM users WHERE id=$1`, id).Scan(&curRole, &curActive)
		if errors.Is(err, pgx.ErrNoRows) {
			return c.Status(404).JSON(fiber.Map{"error": "user not found"})
		}
		if err != nil {
			return serverError(c, err)
		}
		newRole, newActive := curRole, curActive
		if in.Role != nil {
			newRole = *in.Role
		}
		if in.Active != nil {
			newActive = *in.Active
		}
		if curRole == "admin" && curActive && (newRole != "admin" || !newActive) {
			var others int
			if err = tx.QueryRow(ctx,
				`SELECT count(*) FROM users WHERE role='admin' AND active AND id<>$1`, id).Scan(&others); err != nil {
				return serverError(c, err)
			}
			if others == 0 {
				return c.Status(409).JSON(fiber.Map{"error": "cannot demote or disable the last active admin"})
			}
		}
		if _, err = tx.Exec(ctx,
			`UPDATE users SET role=$1, active=$2,
		   token_version = token_version + CASE WHEN $4 THEN 1 ELSE 0 END
		 WHERE id=$3`, newRole, newActive, id, curActive && !newActive); err != nil {
			return serverError(c, err)
		}
		if err = tx.Commit(ctx); err != nil {
			return serverError(c, err)
		}
		auditUser(c, pool, "user_updated", "user:"+strconv.Itoa(id),
			fmt.Sprintf("role %s->%s, active %t->%t", curRole, newRole, curActive, newActive))
		return c.JSON(fiber.Map{"id": id, "role": newRole, "active": newActive})
	})

	// Admin sets a temporary password; the user must change it at next login and old tokens stop working.
	app.Post("/users/:id/reset-password", admin, func(c *fiber.Ctx) error {
		id, err := c.ParamsInt("id")
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid user id"})
		}
		var in struct {
			NewPassword string `json:"new_password"`
		}
		if err := c.BodyParser(&in); err != nil ||
			len(in.NewPassword) < minPasswordLen || len(in.NewPassword) > maxPasswordLen {
			return c.Status(400).JSON(fiber.Map{"error": "new_password must be 8-72 characters"})
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(in.NewPassword), bcrypt.DefaultCost)
		if err != nil {
			return serverError(c, err)
		}
		tag, err := pool.Exec(c.UserContext(),
			`UPDATE users SET password_hash=$1, must_change_password=true, token_version=token_version+1
			 WHERE id=$2`, string(hash), id)
		if err != nil {
			return serverError(c, err)
		}
		if tag.RowsAffected() == 0 {
			return c.Status(404).JSON(fiber.Map{"error": "user not found"})
		}
		auditUser(c, pool, "password_reset", "user:"+strconv.Itoa(id), "")
		return c.JSON(fiber.Map{"status": "password reset, user must change it at next login"})
	})

	// Newest first. Use before_id to page: /audit-log?limit=50&before_id=123
	app.Get("/audit-log", admin, func(c *fiber.Ctx) error {
		limit := c.QueryInt("limit", 50)
		if limit < 1 {
			limit = 1
		}
		if limit > 200 {
			limit = 200
		}
		before := c.QueryInt("before_id", 0)
		rows, err := pool.Query(c.UserContext(),
			`SELECT id, at, actor_id, actor_email, action, target, ip, detail
			 FROM audit_log WHERE ($1::bigint = 0 OR id < $1::bigint)
			 ORDER BY id DESC LIMIT $2`, before, limit)
		if err != nil {
			return serverError(c, err)
		}
		defer rows.Close()
		type entry struct {
			ID         int64     `json:"id"`
			At         time.Time `json:"at"`
			ActorID    *int64    `json:"actor_id"`
			ActorEmail string    `json:"actor_email"`
			Action     string    `json:"action"`
			Target     string    `json:"target"`
			IP         string    `json:"ip"`
			Detail     string    `json:"detail"`
		}
		out := []entry{}
		for rows.Next() {
			var e entry
			if err := rows.Scan(&e.ID, &e.At, &e.ActorID, &e.ActorEmail, &e.Action, &e.Target, &e.IP, &e.Detail); err != nil {
				return serverError(c, err)
			}
			out = append(out, e)
		}
		return c.JSON(out)
	})
}

// serverError logs the real error on the server and returns a generic message to the client.
func serverError(c *fiber.Ctx, err error) error {
	log.Printf("internal error on %s %s: %v", c.Method(), c.Path(), err)
	return c.Status(500).JSON(fiber.Map{"error": "internal error"})
}
