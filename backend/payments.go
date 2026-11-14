package main

// Payment requests for leads that sales is working on (customer pays the product price).
// Wire it up in main.go:
//
//	registerPaymentPublicRoutes(app, pool) // next to registerPublicChatRoutes, BEFORE registerAuthRoutes (no login)
//	registerPaymentRoutes(app, pool)       // after registerLeadRoutes
//
// How it works
//   - Sales (or admin) creates a request for a lead that is qualified or handed_off. The amount is the product price
//     of the lead's campaign, or an explicit amount (negotiated price); the choice is recorded in amount_source.
//   - The customer opens /pay/<token> (public page) and pays through the QR. With the PromptPay QR used now there is
//     no automatic confirmation, so a person marks the payment as paid. That person must not be the one who created it
//     (same rule as content approval). A payment gateway (2C2P) can replace this later behind paymentProvider.
//   - Paid: the lead moves to status closed (only from qualified/handed_off).
//
// PDPA
//   - the public page and the QR carry no customer data (no name, no lead id), only product text, amount and a reference
//   - a payment still pending when the customer withdraws general consent, or the lead is anonymized, is cancelled
//     by a database trigger; payments that were already paid stay (accounting records) and hold no personal data
//   - this is a transactional message, not marketing: withdrawing marketing consent does not cancel a payment request
//
// Off by default: PAYMENTS_ENABLED=1 and PROMPTPAY_ID are needed. PAYMENTS_MOCK=1 adds a fake "pay" endpoint for demos
// and tests ONLY (anyone with the link could mark it paid), never in production.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ---------- provider seam ----------

// payInfo is what the customer needs to pay. QRPayload is the text of a QR code. A gateway provider
// would also return a redirect URL here.
type payInfo struct {
	QRPayload string
}

// paymentProvider is the one place that knows how money is collected. Today: a PromptPay QR made with the shop's own PromptPay ID (no gateway, no automatic confirmation).
// Later: 2C2P (hosted payment page + a webhook that confirms the payment).
type paymentProvider interface {
	Name() string
	Prepare(amount, reference string) (payInfo, error)
}

type promptPayManual struct{ id string }

func (p promptPayManual) Name() string { return "promptpay_manual" }
func (p promptPayManual) Prepare(amount, reference string) (payInfo, error) {
	q, err := BuildPromptPayPayload(p.id, amount)
	return payInfo{QRPayload: q}, err
}

type paymentAPI struct {
	pool     *pgxpool.Pool
	enabled  bool
	mock     bool
	provider paymentProvider
}

var (
	payOnce sync.Once
	payInst *paymentAPI
)

func getPaymentAPI(pool *pgxpool.Pool) *paymentAPI {
	payOnce.Do(func() {
		a := &paymentAPI{pool: pool}
		a.enabled = os.Getenv("PAYMENTS_ENABLED") == "1"
		a.mock = os.Getenv("PAYMENTS_MOCK") == "1"
		if a.enabled {
			id := os.Getenv("PROMPTPAY_ID")
			if err := validPromptPayID(id); err != nil {
				log.Fatalf("payments: PROMPTPAY_ID: %v", err)
			}
			a.provider = promptPayManual{id: id}
			log.Printf("payments: enabled (provider %s)", a.provider.Name())
			if a.mock {
				log.Printf("payments: WARNING PAYMENTS_MOCK=1, anyone with a payment link can mark it paid. Dev only.")
			}
		}
		payInst = a
	})
	return payInst
}

func initPayments(ctx context.Context, pool *pgxpool.Pool) error {
	for _, s := range []string{
		`CREATE TABLE IF NOT EXISTS payments (
			id            BIGSERIAL PRIMARY KEY,
			lead_id       BIGINT NOT NULL REFERENCES leads(id),
			product_id    BIGINT REFERENCES products(id),
			description   TEXT NOT NULL DEFAULT '',
			amount        NUMERIC(12,2) NOT NULL CHECK (amount > 0),
			currency      TEXT NOT NULL DEFAULT 'THB',
			amount_source TEXT NOT NULL,                      -- product_price | manual
			provider      TEXT NOT NULL,
			token         TEXT NOT NULL UNIQUE,               -- random, goes in the public link
			status        TEXT NOT NULL DEFAULT 'pending',    -- pending | paid | expired | cancelled
			expires_at    TIMESTAMPTZ NOT NULL,
			created_by    TEXT NOT NULL,
			created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
			paid_at       TIMESTAMPTZ,
			confirmed_by  TEXT,
			provider_ref  TEXT,
			cancelled_at  TIMESTAMPTZ
		)`,
		`CREATE INDEX IF NOT EXISTS payments_lead_idx ON payments(lead_id)`,
		`CREATE INDEX IF NOT EXISTS payments_status_idx ON payments(status)`,
		// one open request per lead
		`CREATE UNIQUE INDEX IF NOT EXISTS payments_one_pending_uq ON payments(lead_id) WHERE status='pending'`,
		// PDPA: an unpaid request must not outlive the customer's consent (same idea as lead_followups)
		`CREATE OR REPLACE FUNCTION payments_cancel_on_consent() RETURNS trigger AS $$
		BEGIN
		  UPDATE payments SET status='cancelled', cancelled_at=now()
		   WHERE lead_id = NEW.id AND status = 'pending';
		  RETURN NULL;
		END $$ LANGUAGE plpgsql`,
		`DROP TRIGGER IF EXISTS payments_cancel ON leads`,
		`CREATE TRIGGER payments_cancel AFTER UPDATE ON leads FOR EACH ROW
		 WHEN ((NEW.consent_general IS DISTINCT FROM OLD.consent_general
		     OR NEW.consent_withdrawn_at IS DISTINCT FROM OLD.consent_withdrawn_at
		     OR NEW.anonymized_at IS DISTINCT FROM OLD.anonymized_at)
		   AND (NEW.consent_general IS NOT TRUE OR NEW.consent_withdrawn_at IS NOT NULL OR NEW.anonymized_at IS NOT NULL))
		 EXECUTE FUNCTION payments_cancel_on_consent()`,
		// requests that already exist for a lead without consent (missed path) are cancelled once at start-up
		`UPDATE payments p SET status='cancelled', cancelled_at=now()
		  FROM leads l WHERE p.lead_id = l.id AND p.status='pending'
		   AND (l.consent_general IS NOT TRUE OR l.consent_withdrawn_at IS NOT NULL OR l.anonymized_at IS NOT NULL)`,
	} {
		if _, err := pool.Exec(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

func registerPaymentRoutes(app fiber.Router, pool *pgxpool.Pool) {
	if err := initPayments(context.Background(), pool); err != nil {
		log.Fatalf("payment tables: %v", err)
	}
	a := getPaymentAPI(pool)
	sales := requireRole("sales", "admin")
	confirmer := requireRole("marketing", "admin")

	app.Post("/leads/:id/payments", sales, a.create)
	app.Get("/payments", a.list)
	app.Post("/payments/:id/cancel", sales, a.cancel)
	app.Post("/payments/:id/confirm", confirmer, a.confirm)
}

func registerPaymentPublicRoutes(app fiber.Router, pool *pgxpool.Pool) {
	a := getPaymentAPI(pool)
	app.Get("/public/pay/:token", a.publicGet)
	app.Post("/public/pay/:token/mock-pay", a.publicMockPay)
}

// ---------- helpers ----------

func paymentRef(id int64) string { return fmt.Sprintf("LL%06d", id) }

func newPayToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil // 48 hex characters
}

func isPayToken(s string) bool {
	if len(s) != 48 {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

// settlePayment marks a payment paid and closes the lead (only if sales still holds it). Caller owns the transaction
// and has locked the payment row.
func settlePayment(ctx context.Context, tx pgx.Tx, paymentID, leadID int64, by, ref string) error {
	if _, err := tx.Exec(ctx, `UPDATE payments SET status='paid', paid_at=now(), confirmed_by=$2, provider_ref=NULLIF($3,'')
		WHERE id=$1`, paymentID, by, ref); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE leads SET status='closed', updated_at=now()
		WHERE id=$1 AND status IN ('qualified','handed_off')`, leadID)
	return err
}

// ---------- staff endpoints ----------

// POST /leads/:id/payments  {"amount": 11900 (optional), "expires_in_hours": 24 (optional, 1..168)}
func (a *paymentAPI) create(c *fiber.Ctx) error {
	if !a.enabled {
		return leadErr(c, 409, "payments are not enabled (set PAYMENTS_ENABLED=1 and PROMPTPAY_ID)")
	}
	lid, err := leadID(c)
	if err != nil {
		return leadErr(c, 400, "invalid lead id")
	}
	var req struct {
		Amount         *float64 `json:"amount"`
		ExpiresInHours *int     `json:"expires_in_hours"`
	}
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&req); err != nil {
			return leadErr(c, 400, "invalid JSON")
		}
	}
	var amountArg *string // nil = use the product price
	if req.Amount != nil {
		v := *req.Amount
		if math.IsNaN(v) || v <= 0 || v > 10000000 || math.Abs(v*100-math.Round(v*100)) > 1e-6 {
			return leadErr(c, 400, "amount must be greater than 0, at most 10,000,000, with at most 2 decimals")
		}
		s := strconv.FormatFloat(v, 'f', 2, 64)
		amountArg = &s
	}
	hours := 24
	if req.ExpiresInHours != nil {
		hours = *req.ExpiresInHours
		if hours < 1 || hours > 168 {
			return leadErr(c, 400, "expires_in_hours must be between 1 and 168")
		}
	}
	token, err := newPayToken()
	if err != nil {
		return serverError(c, err)
	}
	ctx := c.Context()
	email := currentUser(c).Email

	// an old request that ran out of time must not block a new one
	if _, err := a.pool.Exec(ctx, `UPDATE payments SET status='expired' WHERE lead_id=$1 AND status='pending' AND expires_at <= now()`, lid); err != nil {
		return serverError(c, err)
	}

	// ONE statement checks the lead (consent, status, price) and inserts, so a consent withdrawal cannot
	// slip in between a separate check and the insert. (If one lands right after, the trigger cancels it.)
	var pid int64
	var amount string
	var expires time.Time
	err = a.pool.QueryRow(ctx, `
		INSERT INTO payments(lead_id, product_id, description, amount, amount_source, provider, token, expires_at, created_by)
		SELECT l.id, p.id, COALESCE(p.name,''), COALESCE($2::text::numeric, p.price),
		       CASE WHEN $2::text IS NULL THEN 'product_price' ELSE 'manual' END,
		       $3, $4, now() + make_interval(hours => $5::int), $6
		FROM leads l
		LEFT JOIN campaigns c ON c.id = l.campaign_id
		LEFT JOIN products p ON p.id = c.product_id
		WHERE l.id = $1 AND l.status IN ('qualified','handed_off')
		  AND l.consent_general AND l.consent_withdrawn_at IS NULL AND l.anonymized_at IS NULL
		  AND COALESCE($2::text::numeric, p.price) > 0
		RETURNING id, amount::text, expires_at`,
		lid, amountArg, a.provider.Name(), token, hours, email).Scan(&pid, &amount, &expires)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return leadErr(c, 409, "this lead already has a pending payment request; cancel it first")
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return serverError(c, err)
		}
		// nothing inserted: say why
		var status string
		var consentOK, hasPrice bool
		e := a.pool.QueryRow(ctx, `SELECT l.status,
			   (l.consent_general AND l.consent_withdrawn_at IS NULL AND l.anonymized_at IS NULL),
			   (p.price IS NOT NULL)
			FROM leads l LEFT JOIN campaigns c ON c.id = l.campaign_id LEFT JOIN products p ON p.id = c.product_id
			WHERE l.id = $1`, lid).Scan(&status, &consentOK, &hasPrice)
		if errors.Is(e, pgx.ErrNoRows) {
			return leadErr(c, 404, "lead not found")
		}
		if e != nil {
			return serverError(c, e)
		}
		switch {
		case !consentOK:
			return leadErr(c, 409, "lead has no valid consent (withdrawn or anonymized)")
		case status != "qualified" && status != "handed_off":
			return leadErr(c, 409, "a payment request can only be made for a qualified or handed_off lead")
		case amountArg == nil && !hasPrice:
			return leadErr(c, 400, "the lead's campaign has no product price; send an amount")
		}
		return leadErr(c, 400, "amount must be greater than 0")
	}

	ref := paymentRef(pid)
	info, err := a.provider.Prepare(amount, ref)
	if err != nil {
		return serverError(c, err)
	}
	src := "product_price"
	if amountArg != nil {
		src = "manual"
	}
	auditUser(c, a.pool, "payment_created", "payment:"+strconv.FormatInt(pid, 10),
		"lead:"+strconv.FormatInt(lid, 10)+" amount="+amount+" source="+src)
	return c.Status(201).JSON(fiber.Map{"id": pid, "reference": ref, "amount": amount, "currency": "THB",
		"amount_source": src, "status": "pending", "expires_at": expires,
		"token": token, "pay_path": "/pay/" + token, "qr_payload": info.QRPayload})
}

// GET /payments?status=&lead_id=   (status can be pending|paid|expired|cancelled; a pending one past its time shows as expired)
func (a *paymentAPI) list(c *fiber.Ctx) error {
	status := c.Query("status")
	switch status {
	case "", "pending", "paid", "expired", "cancelled":
	default:
		return leadErr(c, 400, "invalid status")
	}
	leadFilter := c.QueryInt("lead_id", 0)
	rows, err := a.pool.Query(c.Context(), `
		SELECT x.id, x.lead_id, x.display_name, x.description, x.amount, x.currency, x.amount_source, x.provider,
		       x.eff, x.expires_at, x.created_by, x.created_at, x.paid_at, x.confirmed_by, x.provider_ref, x.token
		FROM (
		  SELECT p.id, p.lead_id, l.display_name, p.description, p.amount::float8 AS amount, p.currency, p.amount_source,
		         p.provider, p.expires_at, p.created_by, p.created_at, p.paid_at, p.confirmed_by, p.provider_ref, p.token,
		         CASE WHEN p.status='pending' AND p.expires_at <= now() THEN 'expired' ELSE p.status END AS eff
		  FROM payments p JOIN leads l ON l.id = p.lead_id
		  WHERE ($2::bigint = 0 OR p.lead_id = $2)
		) x
		WHERE ($1::text = '' OR x.eff = $1)
		ORDER BY x.id DESC LIMIT 200`, status, leadFilter)
	if err != nil {
		return serverError(c, err)
	}
	defer rows.Close()
	out := []fiber.Map{}
	for rows.Next() {
		var id, lid int64
		var name, desc, cur, src, prov, eff, createdBy, token string
		var amount float64
		var expires, created time.Time
		var paidAt *time.Time
		var confirmedBy, provRef *string
		if err := rows.Scan(&id, &lid, &name, &desc, &amount, &cur, &src, &prov, &eff, &expires, &createdBy,
			&created, &paidAt, &confirmedBy, &provRef, &token); err != nil {
			return serverError(c, err)
		}
		m := fiber.Map{"id": id, "reference": paymentRef(id), "lead_id": lid, "display_name": name, "description": desc,
			"amount": amount, "currency": cur, "amount_source": src, "provider": prov, "status": eff,
			"expires_at": expires, "created_by": createdBy, "created_at": created, "paid_at": paidAt,
			"confirmed_by": confirmedBy, "provider_ref": provRef}
		if eff == "pending" { // only an open request exposes its link
			m["pay_path"] = "/pay/" + token
		}
		out = append(out, m)
	}
	return c.JSON(out)
}

// POST /payments/:id/cancel
func (a *paymentAPI) cancel(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return leadErr(c, 400, "invalid payment id")
	}
	tag, err := a.pool.Exec(c.Context(), `UPDATE payments SET status='cancelled', cancelled_at=now()
		WHERE id=$1 AND status='pending'`, id)
	if err != nil {
		return serverError(c, err)
	}
	if tag.RowsAffected() == 0 {
		return leadErr(c, 409, "payment not found or not pending")
	}
	auditUser(c, a.pool, "payment_cancelled", "payment:"+strconv.Itoa(id), "")
	return c.JSON(fiber.Map{"status": "cancelled"})
}

// POST /payments/:id/confirm  {"reference": "bank slip or transfer reference (optional)"}
// A person confirms that the money arrived. The confirmer must be a different person from the creator.
// A request that ran out of time (expired) can still be confirmed because money may arrive late;
// a cancelled one cannot (handle it outside the system, for example with a refund).
func (a *paymentAPI) confirm(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return leadErr(c, 400, "invalid payment id")
	}
	var req struct {
		Reference string `json:"reference"`
	}
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&req); err != nil {
			return leadErr(c, 400, "invalid JSON")
		}
	}
	ref := strings.TrimSpace(req.Reference)
	if len(ref) > 100 {
		return leadErr(c, 400, "reference must be at most 100 characters")
	}
	email := currentUser(c).Email
	ctx := c.Context()
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return serverError(c, err)
	}
	defer tx.Rollback(ctx)

	var creator, status string
	var lid int64
	err = tx.QueryRow(ctx, `SELECT created_by, status, lead_id FROM payments WHERE id=$1 FOR UPDATE`, id).Scan(&creator, &status, &lid)
	if errors.Is(err, pgx.ErrNoRows) {
		return leadErr(c, 404, "payment not found")
	}
	if err != nil {
		return serverError(c, err)
	}
	if strings.EqualFold(creator, email) {
		auditUser(c, a.pool, "payment_confirm_denied", "payment:"+strconv.Itoa(id), "confirmer is the creator")
		return leadErr(c, 403, "the person who confirms a payment must be different from the person who created the request")
	}
	if status != "pending" && status != "expired" {
		return leadErr(c, 409, "payment is "+status+"; only pending (or expired) payments can be confirmed")
	}
	if err := settlePayment(ctx, tx, int64(id), lid, email, ref); err != nil {
		return serverError(c, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return serverError(c, err)
	}
	auditUser(c, a.pool, "payment_paid", "payment:"+strconv.Itoa(id), "lead:"+strconv.FormatInt(lid, 10))
	return c.JSON(fiber.Map{"status": "paid"})
}

// ---------- public endpoints (no login; the random token is the key) ----------

func payNotFound(c *fiber.Ctx) error {
	return c.Status(404).JSON(fiber.Map{"error": "not found"})
}

// GET /public/pay/:token: what the customer's page needs. No customer data, no lead id.
func (a *paymentAPI) publicGet(c *fiber.Ctx) error {
	token := c.Params("token")
	if !a.enabled || !isPayToken(token) {
		return payNotFound(c)
	}
	var id int64
	var desc, amount, cur, status string
	var expires time.Time
	err := a.pool.QueryRow(c.Context(), `
		SELECT id, description, amount::text, currency, expires_at,
		       CASE WHEN status='pending' AND expires_at <= now() THEN 'expired' ELSE status END
		FROM payments WHERE token=$1`, token).Scan(&id, &desc, &amount, &cur, &expires, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return payNotFound(c)
	}
	if err != nil {
		return serverError(c, err)
	}
	c.Set("Cache-Control", "no-store")
	out := fiber.Map{"reference": paymentRef(id), "description": desc, "amount": amount, "currency": cur,
		"status": status, "expires_at": expires, "mock_pay": a.mock}
	if status == "pending" {
		info, err := a.provider.Prepare(amount, paymentRef(id))
		if err != nil {
			return serverError(c, err)
		}
		out["qr_payload"] = info.QRPayload
	}
	return c.JSON(out)
}

// POST /public/pay/:token/mock-pay: demo/test only (PAYMENTS_MOCK=1). Stands in for a gateway's "payment succeeded".
func (a *paymentAPI) publicMockPay(c *fiber.Ctx) error {
	token := c.Params("token")
	if !a.enabled || !a.mock || !isPayToken(token) {
		return payNotFound(c)
	}
	ctx := c.Context()
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return serverError(c, err)
	}
	defer tx.Rollback(ctx)
	var id, lid int64
	var status string
	var expires time.Time
	err = tx.QueryRow(ctx, `SELECT id, lead_id, status, expires_at FROM payments WHERE token=$1 FOR UPDATE`, token).Scan(&id, &lid, &status, &expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return payNotFound(c)
	}
	if err != nil {
		return serverError(c, err)
	}
	if status != "pending" || !expires.After(time.Now()) {
		return c.Status(409).JSON(fiber.Map{"error": "payment is not open"})
	}
	if err := settlePayment(ctx, tx, id, lid, "mock-provider", "mock"); err != nil {
		return serverError(c, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return serverError(c, err)
	}
	return c.JSON(fiber.Map{"status": "paid"})
}
