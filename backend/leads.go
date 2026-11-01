package main

// Leads + scoring-rule endpoints. Wire it up in main.go, after registerAuthRoutes:
//
//	registerLeadRoutes(app, pool)
//
// Uses the existing helpers: requireRole, currentUser(c).Email, auditUser, serverError.

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type leadAPI struct{ pool *pgxpool.Pool }

func registerLeadRoutes(app fiber.Router, pool *pgxpool.Pool) {
	a := &leadAPI{pool: pool}
	mk := requireRole("marketing", "admin")
	adm := requireRole("admin")
	sales := requireRole("sales", "admin")

	app.Post("/leads", mk, a.createLead)
	app.Get("/leads", a.listLeads)
	app.Get("/leads/:id", a.getLead)
	app.Post("/leads/:id/events", mk, a.addLeadEvent)
	app.Post("/leads/:id/message", mk, a.addLeadMessage)
	app.Post("/leads/:id/consent", mk, a.setConsent)
	app.Post("/leads/:id/status", sales, a.setLeadStatus)
	app.Get("/leads/:id/export", adm, a.exportLead)
	app.Delete("/leads/:id", adm, a.anonymizeLead)

	app.Get("/scoring-rules", a.listRules)
	app.Post("/scoring-rules", mk, a.createRule)
	app.Post("/scoring-rules/test", mk, a.testRules)
	app.Post("/scoring-rules/recalculate", adm, a.recalcAll)
	app.Patch("/scoring-rules/:id", mk, a.updateRule)
	app.Delete("/scoring-rules/:id", mk, a.deleteRule)
	app.Get("/scoring-settings", a.listSettings)
	app.Patch("/scoring-settings", adm, a.patchSettings)
	app.Get("/event-keywords", a.listKeywords)
	app.Put("/event-keywords", mk, a.replaceKeywords)
}

func leadErr(c *fiber.Ctx, status int, msg string) error {
	return c.Status(status).JSON(fiber.Map{"error": msg})
}

func auditJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// ---------- loading + rescoring ----------

func (a *leadAPI) loadSettings(ctx context.Context) (ScoringSettings, error) {
	s := DefaultScoringSettings()
	rows, err := a.pool.Query(ctx, `SELECT key, value FROM scoring_settings`)
	if err != nil {
		return s, err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var v json.RawMessage
		if err := rows.Scan(&k, &v); err != nil {
			return s, err
		}
		switch k {
		case "handoff_threshold":
			_ = json.Unmarshal(v, &s.HandoffThreshold)
		case "level_warm_min":
			_ = json.Unmarshal(v, &s.WarmMin)
		case "score_min":
			_ = json.Unmarshal(v, &s.ScoreMin)
		case "score_max":
			_ = json.Unmarshal(v, &s.ScoreMax)
		case "immediate_handoff_events":
			_ = json.Unmarshal(v, &s.ImmediateHandoff)
		case "disqualify_events":
			_ = json.Unmarshal(v, &s.Disqualify)
		}
	}
	return s, rows.Err()
}

func (a *leadAPI) loadRules(ctx context.Context) ([]ScoringRule, error) {
	rows, err := a.pool.Query(ctx,
		`SELECT id, name, condition, points, mode, max_total, active, sort_order FROM scoring_rules`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ScoringRule
	for rows.Next() {
		var r ScoringRule
		if err := rows.Scan(&r.ID, &r.Name, &r.Condition, &r.Points, &r.Mode, &r.MaxTotal, &r.Active, &r.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (a *leadAPI) loadKeywords(ctx context.Context) (map[string][]string, error) {
	rows, err := a.pool.Query(ctx, `SELECT event_type, keyword FROM event_keywords`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string][]string{}
	for rows.Next() {
		var e, k string
		if err := rows.Scan(&e, &k); err != nil {
			return nil, err
		}
		m[e] = append(m[e], k)
	}
	return m, rows.Err()
}

func (a *leadAPI) buildFacts(ctx context.Context, leadID int64) (*LeadFacts, string, error) {
	var budget, price *float64
	var channel, status string
	err := a.pool.QueryRow(ctx, `
		SELECT l.budget::float8, l.channel, l.status, p.price::float8
		FROM leads l
		LEFT JOIN campaigns c ON c.id = l.campaign_id
		LEFT JOIN products p ON p.id = c.product_id
		WHERE l.id = $1`, leadID).Scan(&budget, &channel, &status, &price)
	if err != nil {
		return nil, "", err
	}
	f := &LeadFacts{Lead: map[string]any{"channel": channel}, ProductPrice: price}
	if budget != nil && *budget > 0 { // unknown budget stays ABSENT (never 0)
		f.Lead["budget"] = *budget
	}
	rows, err := a.pool.Query(ctx, `SELECT type, data FROM lead_events WHERE lead_id=$1 ORDER BY id`, leadID)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	for rows.Next() {
		var e LeadEvent
		var raw []byte
		if err := rows.Scan(&e.Type, &raw); err != nil {
			return nil, "", err
		}
		_ = json.Unmarshal(raw, &e.Data)
		f.Events = append(f.Events, e)
	}
	return f, status, rows.Err()
}

// rescore recomputes one lead, stores the explanation, and moves the status if needed.
func (a *leadAPI) rescore(ctx context.Context, leadID int64) (ScoreResult, error) {
	settings, err := a.loadSettings(ctx)
	if err != nil {
		return ScoreResult{}, err
	}
	rules, err := a.loadRules(ctx)
	if err != nil {
		return ScoreResult{}, err
	}
	facts, status, err := a.buildFacts(ctx, leadID)
	if err != nil {
		return ScoreResult{}, err
	}
	res := ScoreLead(rules, settings, facts)

	newStatus := status
	switch {
	case res.Disqualified && status != "closed":
		newStatus = "lost"
	case res.Qualified && (status == "new" || status == "nurturing"):
		newStatus = "qualified"
	}

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return res, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `DELETE FROM lead_score_log WHERE lead_id=$1`, leadID); err != nil {
		return res, err
	}
	for _, ap := range res.Breakdown {
		if _, err = tx.Exec(ctx, `INSERT INTO lead_score_log(lead_id, rule_id, rule_name, points) VALUES($1,$2,$3,$4)`,
			leadID, ap.RuleID, ap.RuleName, ap.Points); err != nil {
			return res, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE leads SET score=$2, level=$3, status=$4, updated_at=now() WHERE id=$1`,
		leadID, res.Score, res.Level, newStatus); err != nil {
		return res, err
	}
	return res, tx.Commit(ctx)
}

func leadID(c *fiber.Ctx) (int64, error) {
	n, err := c.ParamsInt("id")
	return int64(n), err
}

// leadAllowsProcessing: consent given, not withdrawn, not anonymized.
func (a *leadAPI) leadAllowsProcessing(ctx context.Context, id int64) (bool, error) {
	var ok bool
	err := a.pool.QueryRow(ctx,
		`SELECT consent_general AND consent_withdrawn_at IS NULL AND anonymized_at IS NULL FROM leads WHERE id=$1`, id).Scan(&ok)
	return ok, err
}

// ---------- leads ----------

func (a *leadAPI) createLead(c *fiber.Ctx) error {
	var req struct {
		CampaignID     *int64   `json:"campaign_id"`
		DisplayName    string   `json:"display_name"`
		Channel        string   `json:"channel"`
		ChannelUserID  string   `json:"channel_user_id"`
		Budget         *float64 `json:"budget"`
		ConsentGeneral bool     `json:"consent_general"`
		ConsentMarket  bool     `json:"consent_marketing"`
		ConsentVersion string   `json:"consent_version"`
	}
	if err := c.BodyParser(&req); err != nil {
		return leadErr(c, 400, "invalid JSON")
	}
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	if req.DisplayName == "" || (req.Channel != "line" && req.Channel != "mock" && req.Channel != "web") {
		return leadErr(c, 400, "display_name and channel (line|mock|web) are required")
	}
	// PDPA: nothing is stored without consent.
	if !req.ConsentGeneral || strings.TrimSpace(req.ConsentVersion) == "" {
		return leadErr(c, 400, "consent_general=true and consent_version are required before a lead can be stored")
	}
	if req.Budget != nil && *req.Budget < 0 {
		return leadErr(c, 400, "budget must not be negative")
	}
	ctx := c.Context()
	if req.CampaignID != nil {
		var ok bool
		if err := a.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM campaigns WHERE id=$1)`, *req.CampaignID).Scan(&ok); err != nil {
			return serverError(c, err)
		}
		if !ok {
			return leadErr(c, 404, "campaign not found")
		}
	}
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return serverError(c, err)
	}
	defer tx.Rollback(ctx)
	var id int64
	err = tx.QueryRow(ctx, `
		INSERT INTO leads(campaign_id, display_name, channel, channel_user_id, budget,
		  consent_general, consent_marketing, consent_version, consent_at, created_by)
		VALUES($1,$2,$3,NULLIF($4,''),$5,TRUE,$6,$7,now(),$8) RETURNING id`,
		req.CampaignID, req.DisplayName, req.Channel, req.ChannelUserID, req.Budget,
		req.ConsentMarket, req.ConsentVersion, currentUser(c).Email).Scan(&id)
	if err != nil {
		return serverError(c, err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO consent_events(lead_id, kind, action, text_version) VALUES($1,'general','granted',$2)`, id, req.ConsentVersion); err != nil {
		return serverError(c, err)
	}
	if req.ConsentMarket {
		if _, err = tx.Exec(ctx, `INSERT INTO consent_events(lead_id, kind, action, text_version) VALUES($1,'marketing','granted',$2)`, id, req.ConsentVersion); err != nil {
			return serverError(c, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return serverError(c, err)
	}
	auditUser(c, a.pool, "lead_created", "lead:"+strconv.FormatInt(id, 10), "")
	res, err := a.rescore(ctx, id)
	if err != nil {
		return serverError(c, err)
	}
	return c.Status(201).JSON(fiber.Map{"id": id, "score": res.Score, "level": res.Level})
}

func (a *leadAPI) listLeads(c *fiber.Ctx) error {
	rows, err := a.pool.Query(c.Context(), `
		SELECT id, campaign_id, display_name, channel, budget::float8, status, score, level, consent_marketing, created_at
		FROM leads WHERE anonymized_at IS NULL
		  AND ($1 = '' OR status = $1) AND ($2 = '' OR level = $2) AND ($3 = '' OR channel = $3)
		ORDER BY score DESC, id DESC LIMIT 200`,
		c.Query("status"), c.Query("level"), c.Query("channel"))
	if err != nil {
		return serverError(c, err)
	}
	defer rows.Close()
	out := []fiber.Map{}
	for rows.Next() {
		var id int64
		var score int
		var campaign *int64
		var name, channel, status, level string
		var budget *float64
		var consentM bool
		var created time.Time
		if err := rows.Scan(&id, &campaign, &name, &channel, &budget, &status, &score, &level, &consentM, &created); err != nil {
			return serverError(c, err)
		}
		out = append(out, fiber.Map{"id": id, "campaign_id": campaign, "display_name": name, "channel": channel,
			"budget": budget, "status": status, "score": score, "level": level,
			"consent_marketing": consentM, "created_at": created})
	}
	return c.JSON(out)
}

func (a *leadAPI) getLead(c *fiber.Ctx) error {
	id, err := leadID(c)
	if err != nil {
		return leadErr(c, 400, "invalid lead id")
	}
	var name, channel, status, level string
	var score int
	var budget *float64
	var consentG, consentM bool
	err = a.pool.QueryRow(c.Context(), `SELECT display_name, channel, budget::float8, status, score, level, consent_general, consent_marketing
		FROM leads WHERE id=$1 AND anonymized_at IS NULL`, id).Scan(&name, &channel, &budget, &status, &score, &level, &consentG, &consentM)
	if errors.Is(err, pgx.ErrNoRows) {
		return leadErr(c, 404, "lead not found")
	}
	if err != nil {
		return serverError(c, err)
	}
	why := []fiber.Map{}
	rows, err := a.pool.Query(c.Context(), `SELECT rule_name, points FROM lead_score_log WHERE lead_id=$1 ORDER BY id`, id)
	if err != nil {
		return serverError(c, err)
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		var p int
		if err := rows.Scan(&n, &p); err != nil {
			return serverError(c, err)
		}
		why = append(why, fiber.Map{"rule": n, "points": p})
	}
	return c.JSON(fiber.Map{"id": id, "display_name": name, "channel": channel, "budget": budget, "status": status,
		"score": score, "level": level, "consent_general": consentG, "consent_marketing": consentM, "score_breakdown": why})
}

type leadEventReq struct {
	Type string         `json:"type"`
	Data map[string]any `json:"data"`
}

func (a *leadAPI) addLeadEvent(c *fiber.Ctx) error {
	id, err := leadID(c)
	if err != nil {
		return leadErr(c, 400, "invalid lead id")
	}
	var req leadEventReq
	if err := c.BodyParser(&req); err != nil || strings.TrimSpace(req.Type) == "" {
		return leadErr(c, 400, "type is required")
	}
	return a.recordEvents(c, id, []leadEventReq{req})
}

// POST /leads/:id/message {"text": "..."}: detect events from keywords. The text itself is NOT stored.
func (a *leadAPI) addLeadMessage(c *fiber.Ctx) error {
	id, err := leadID(c)
	if err != nil {
		return leadErr(c, 400, "invalid lead id")
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := c.BodyParser(&req); err != nil || strings.TrimSpace(req.Text) == "" {
		return leadErr(c, 400, "text is required")
	}
	// Check consent before processing the submitted text, even if no keyword is detected.
	ok, err := a.leadAllowsProcessing(c.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		return leadErr(c, 404, "lead not found")
	}
	if err != nil {
		return serverError(c, err)
	}
	if !ok {
		return leadErr(c, 409, "lead has no valid consent (withdrawn or anonymized)")
	}
	kw, err := a.loadKeywords(c.Context())
	if err != nil {
		return serverError(c, err)
	}
	var evs []leadEventReq
	for _, t := range DetectEvents(req.Text, kw) {
		evs = append(evs, leadEventReq{Type: t})
	}
	if len(evs) == 0 {
		return c.JSON(fiber.Map{"detected": []string{}})
	}
	return a.recordEvents(c, id, evs)
}

// applyEvents stores events and rescores the lead. It has no HTTP in it, so the chat channels can use it too.
// The caller must already have checked consent (leadAllowsProcessing).
func (a *leadAPI) applyEvents(ctx context.Context, id int64, evs []leadEventReq) ([]string, ScoreResult, error) {
	detected := []string{}
	for _, e := range evs {
		data, err := json.Marshal(e.Data)
		if err != nil || string(data) == "null" {
			data = []byte("{}")
		}
		if _, err := a.pool.Exec(ctx, `INSERT INTO lead_events(lead_id, type, data) VALUES($1,$2,$3)`, id, e.Type, string(data)); err != nil {
			return nil, ScoreResult{}, err
		}
		detected = append(detected, e.Type)
		if e.Type == "unsubscribed" { // stop marketing at once, keep the evidence
			if _, err := a.pool.Exec(ctx, `UPDATE leads SET consent_marketing=FALSE, updated_at=now() WHERE id=$1`, id); err != nil {
				return nil, ScoreResult{}, err
			}
			if _, err := a.pool.Exec(ctx, `INSERT INTO consent_events(lead_id, kind, action, text_version) VALUES($1,'marketing','withdrawn','keyword')`, id); err != nil {
				return nil, ScoreResult{}, err
			}
		}
	}
	res, err := a.rescore(ctx, id)
	if err != nil {
		return nil, ScoreResult{}, err
	}
	return detected, res, nil
}

func (a *leadAPI) recordEvents(c *fiber.Ctx, id int64, evs []leadEventReq) error {
	ctx := c.Context()
	ok, err := a.leadAllowsProcessing(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return leadErr(c, 404, "lead not found")
	}
	if err != nil {
		return serverError(c, err)
	}
	if !ok {
		return leadErr(c, 409, "lead has no valid consent (withdrawn or anonymized)")
	}
	detected, res, err := a.applyEvents(ctx, id, evs)
	if err != nil {
		return serverError(c, err)
	}
	return c.JSON(fiber.Map{"detected": detected, "score": res.Score, "level": res.Level,
		"qualified": res.Qualified, "immediate_handoff": res.ImmediateHandoff,
		"disqualified": res.Disqualified, "breakdown": res.Breakdown})
}

func (a *leadAPI) setConsent(c *fiber.Ctx) error {
	id, err := leadID(c)
	if err != nil {
		return leadErr(c, 400, "invalid lead id")
	}
	var req struct {
		Kind        string `json:"kind"`   // general | marketing
		Action      string `json:"action"` // granted | withdrawn
		TextVersion string `json:"text_version"`
	}
	if err := c.BodyParser(&req); err != nil || (req.Kind != "general" && req.Kind != "marketing") ||
		(req.Action != "granted" && req.Action != "withdrawn") || strings.TrimSpace(req.TextVersion) == "" {
		return leadErr(c, 400, "kind (general|marketing), action (granted|withdrawn) and text_version are required")
	}
	ctx := c.Context()
	granted := req.Action == "granted"
	var tagErr error
	var affected int64
	switch {
	case req.Kind == "marketing" && granted:
		// marketing consent needs valid general consent first
		tag, e := a.pool.Exec(ctx, `UPDATE leads SET consent_marketing=TRUE, updated_at=now()
			WHERE id=$1 AND anonymized_at IS NULL AND consent_general AND consent_withdrawn_at IS NULL`, id)
		tagErr, affected = e, tag.RowsAffected()
	case req.Kind == "marketing":
		tag, e := a.pool.Exec(ctx, `UPDATE leads SET consent_marketing=FALSE, updated_at=now() WHERE id=$1 AND anonymized_at IS NULL`, id)
		tagErr, affected = e, tag.RowsAffected()
	case granted:
		tag, e := a.pool.Exec(ctx, `UPDATE leads SET consent_general=TRUE, consent_withdrawn_at=NULL, consent_version=$2, consent_at=now(), updated_at=now()
			WHERE id=$1 AND anonymized_at IS NULL`, id, req.TextVersion)
		tagErr, affected = e, tag.RowsAffected()
	default: // withdrawing general consent also ends marketing consent
		tag, e := a.pool.Exec(ctx, `UPDATE leads SET consent_general=FALSE, consent_marketing=FALSE, consent_withdrawn_at=now(), updated_at=now()
			WHERE id=$1 AND anonymized_at IS NULL`, id)
		tagErr, affected = e, tag.RowsAffected()
	}
	if tagErr != nil {
		return serverError(c, tagErr)
	}
	if affected == 0 {
		return leadErr(c, 409, "lead not found, anonymized, or marketing consent requires valid general consent")
	}
	if _, err := a.pool.Exec(ctx, `INSERT INTO consent_events(lead_id, kind, action, text_version) VALUES($1,$2,$3,$4)`,
		id, req.Kind, req.Action, req.TextVersion); err != nil {
		return serverError(c, err)
	}
	auditUser(c, a.pool, "lead_consent_"+req.Action, "lead:"+strconv.FormatInt(id, 10), req.Kind)
	return c.JSON(fiber.Map{"ok": true})
}

func (a *leadAPI) setLeadStatus(c *fiber.Ctx) error {
	id, err := leadID(c)
	if err != nil {
		return leadErr(c, 400, "invalid lead id")
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := c.BodyParser(&req); err != nil || (req.Status != "handed_off" && req.Status != "closed" && req.Status != "lost") {
		return leadErr(c, 400, "status must be handed_off, closed or lost")
	}
	// a lead can only be taken when it is qualified; closed/lost only from qualified or handed_off
	tag, err := a.pool.Exec(c.Context(), `UPDATE leads SET status=$2, updated_at=now()
		WHERE id=$1 AND anonymized_at IS NULL
		  AND (($2='handed_off' AND status='qualified') OR ($2<>'handed_off' AND status IN ('qualified','handed_off')))`, id, req.Status)
	if err != nil {
		return serverError(c, err)
	}
	if tag.RowsAffected() == 0 {
		return leadErr(c, 409, "lead not found or this status change is not allowed from its current status")
	}
	auditUser(c, a.pool, "lead_status_changed", "lead:"+strconv.FormatInt(id, 10), req.Status)
	return c.JSON(fiber.Map{"status": req.Status})
}

func (a *leadAPI) exportLead(c *fiber.Ctx) error {
	id, err := leadID(c)
	if err != nil {
		return leadErr(c, 400, "invalid lead id")
	}
	ctx := c.Context()
	var name, channel, status, level string
	var score int
	var budget *float64
	var created time.Time
	err = a.pool.QueryRow(ctx, `SELECT display_name, channel, budget::float8, status, score, level, created_at
		FROM leads WHERE id=$1 AND anonymized_at IS NULL`, id).Scan(&name, &channel, &budget, &status, &score, &level, &created)
	if errors.Is(err, pgx.ErrNoRows) {
		return leadErr(c, 404, "lead not found")
	}
	if err != nil {
		return serverError(c, err)
	}
	events, consents := []fiber.Map{}, []fiber.Map{}

	er, err := a.pool.Query(ctx, `SELECT type, data, created_at FROM lead_events WHERE lead_id=$1 ORDER BY id`, id)
	if err != nil {
		return serverError(c, err)
	}
	for er.Next() {
		var t string
		var d json.RawMessage
		var at time.Time
		if err := er.Scan(&t, &d, &at); err != nil {
			er.Close()
			return serverError(c, err)
		}
		events = append(events, fiber.Map{"type": t, "data": d, "at": at})
	}
	er.Close()

	cr, err := a.pool.Query(ctx, `SELECT kind, action, text_version, created_at FROM consent_events WHERE lead_id=$1 ORDER BY id`, id)
	if err != nil {
		return serverError(c, err)
	}
	for cr.Next() {
		var k, ac, v string
		var at time.Time
		if err := cr.Scan(&k, &ac, &v, &at); err != nil {
			cr.Close()
			return serverError(c, err)
		}
		consents = append(consents, fiber.Map{"kind": k, "action": ac, "text_version": v, "at": at})
	}
	cr.Close()

	auditUser(c, a.pool, "lead_exported", "lead:"+strconv.FormatInt(id, 10), "")
	return c.JSON(fiber.Map{
		"lead":     fiber.Map{"id": id, "display_name": name, "channel": channel, "budget": budget, "status": status, "score": score, "level": level, "created_at": created},
		"events":   events,
		"consents": consents,
	})
}

// DELETE /leads/:id anonymizes: personal data is removed, aggregate numbers stay.
func (a *leadAPI) anonymizeLead(c *fiber.Ctx) error {
	id, err := leadID(c)
	if err != nil {
		return leadErr(c, 400, "invalid lead id")
	}
	ctx := c.Context()
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return serverError(c, err)
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE leads SET display_name='(deleted)', name='', contact='', channel_user_id=NULL, budget=NULL,
		consent=FALSE, consent_general=FALSE, consent_marketing=FALSE,
		consent_withdrawn_at=COALESCE(consent_withdrawn_at, now()), anonymized_at=now(), updated_at=now()
		WHERE id=$1 AND anonymized_at IS NULL`, id)
	if err != nil {
		return serverError(c, err)
	}
	if tag.RowsAffected() == 0 {
		return leadErr(c, 404, "lead not found")
	}
	if _, err := tx.Exec(ctx, `DELETE FROM lead_events WHERE lead_id=$1`, id); err != nil {
		return serverError(c, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return serverError(c, err)
	}
	auditUser(c, a.pool, "lead_anonymized", "lead:"+strconv.FormatInt(id, 10), "")
	return c.JSON(fiber.Map{"ok": true})
}

// ---------- scoring rules ----------

type scoringRuleReq struct {
	Name        string          `json:"name"`
	Description *string         `json:"description"`
	Category    string          `json:"category"`
	Condition   json.RawMessage `json:"condition"`
	Points      *int            `json:"points"`
	Mode        string          `json:"mode"`
	MaxTotal    *int            `json:"max_total"`
	Active      *bool           `json:"active"`
	SortOrder   *int            `json:"sort_order"`
}

func (a *leadAPI) listRules(c *fiber.Ctx) error {
	rows, err := a.pool.Query(c.Context(), `SELECT id, name, description, category, is_core, condition, points, mode, max_total, active, sort_order
		FROM scoring_rules ORDER BY sort_order, id`)
	if err != nil {
		return serverError(c, err)
	}
	defer rows.Close()
	out := []fiber.Map{}
	for rows.Next() {
		var id int64
		var name, cat, mode string
		var desc *string
		var core, active bool
		var cond json.RawMessage
		var pts, order int
		var max *int
		if err := rows.Scan(&id, &name, &desc, &cat, &core, &cond, &pts, &mode, &max, &active, &order); err != nil {
			return serverError(c, err)
		}
		out = append(out, fiber.Map{"id": id, "name": name, "description": desc, "category": cat, "is_core": core,
			"condition": cond, "points": pts, "mode": mode, "max_total": max, "active": active, "sort_order": order})
	}
	return c.JSON(out)
}

func validateRuleReq(r *scoringRuleReq, creating bool) string {
	if creating && (strings.TrimSpace(r.Name) == "" || r.Points == nil || len(r.Condition) == 0) {
		return "name, condition and points are required"
	}
	if len(r.Condition) > 0 {
		if _, err := ParseCondition(r.Condition); err != nil {
			return err.Error()
		}
	}
	if r.Mode != "" && r.Mode != "once" && r.Mode != "each" {
		return "mode must be once or each"
	}
	switch r.Category {
	case "", "intent", "fit", "engagement", "negative":
	default:
		return "category must be intent, fit, engagement or negative"
	}
	if r.Points != nil && (*r.Points < -100 || *r.Points > 100) {
		return "points must be between -100 and 100"
	}
	return ""
}

func (a *leadAPI) createRule(c *fiber.Ctx) error {
	var r scoringRuleReq
	if err := c.BodyParser(&r); err != nil {
		return leadErr(c, 400, "invalid JSON")
	}
	if msg := validateRuleReq(&r, true); msg != "" {
		return leadErr(c, 400, msg)
	}
	if r.Mode == "" {
		r.Mode = "once"
	}
	if r.Category == "" {
		r.Category = "intent"
	}
	active := r.Active == nil || *r.Active
	order := 1000
	if r.SortOrder != nil {
		order = *r.SortOrder
	}
	var id int64
	err := a.pool.QueryRow(c.Context(), `INSERT INTO scoring_rules(name, description, category, condition, points, mode, max_total, active, sort_order, updated_by)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`,
		r.Name, r.Description, r.Category, string(r.Condition), *r.Points, r.Mode, r.MaxTotal, active, order, currentUser(c).Email).Scan(&id)
	if err != nil {
		return serverError(c, err)
	}
	auditUser(c, a.pool, "rule_created", "rule:"+strconv.FormatInt(id, 10), auditJSON(fiber.Map{"name": r.Name, "points": *r.Points}))
	return c.Status(201).JSON(fiber.Map{"id": id, "note": "call POST /scoring-rules/recalculate (admin) to apply the change to existing leads"})
}

func (a *leadAPI) updateRule(c *fiber.Ctx) error {
	id, err := leadID(c)
	if err != nil {
		return leadErr(c, 400, "invalid rule id")
	}
	var r scoringRuleReq
	if err := c.BodyParser(&r); err != nil {
		return leadErr(c, 400, "invalid JSON")
	}
	if msg := validateRuleReq(&r, false); msg != "" {
		return leadErr(c, 400, msg)
	}
	var before json.RawMessage
	var isCore bool
	err = a.pool.QueryRow(c.Context(), `SELECT to_jsonb(s), is_core FROM scoring_rules s WHERE id=$1`, id).Scan(&before, &isCore)
	if errors.Is(err, pgx.ErrNoRows) {
		return leadErr(c, 404, "rule not found")
	}
	if err != nil {
		return serverError(c, err)
	}
	if isCore && r.Active != nil && !*r.Active {
		return leadErr(c, 409, "core scoring rules cannot be disabled")
	}
	var cond any
	if len(r.Condition) > 0 {
		cond = string(r.Condition)
	}
	_, err = a.pool.Exec(c.Context(), `UPDATE scoring_rules SET
		name=COALESCE(NULLIF($2,''),name), description=COALESCE($3,description), category=COALESCE(NULLIF($4,''),category),
		condition=COALESCE($5::jsonb,condition), points=COALESCE($6,points), mode=COALESCE(NULLIF($7,''),mode),
		max_total=COALESCE($8,max_total), active=COALESCE($9,active), sort_order=COALESCE($10,sort_order),
		updated_by=$11, updated_at=now() WHERE id=$1`,
		id, r.Name, r.Description, r.Category, cond, r.Points, r.Mode, r.MaxTotal, r.Active, r.SortOrder, currentUser(c).Email)
	if err != nil {
		return serverError(c, err)
	}
	auditUser(c, a.pool, "rule_updated", "rule:"+strconv.FormatInt(id, 10), auditJSON(fiber.Map{"before": before, "changes": r}))
	return c.JSON(fiber.Map{"ok": true, "note": "call POST /scoring-rules/recalculate (admin) to apply the change to existing leads"})
}

// DELETE: soft delete (active=false) when the rule has ever scored a lead, so lead_score_log stays explainable.
func (a *leadAPI) deleteRule(c *fiber.Ctx) error {
	id, err := leadID(c)
	if err != nil {
		return leadErr(c, 400, "invalid rule id")
	}
	var used, isCore bool
	if err := a.pool.QueryRow(c.Context(), `SELECT is_core, EXISTS(SELECT 1 FROM lead_score_log WHERE rule_id=$1) FROM scoring_rules WHERE id=$1`, id).Scan(&isCore, &used); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return leadErr(c, 404, "rule not found")
		}
		return serverError(c, err)
	}
	if isCore {
		return leadErr(c, 409, "core scoring rules cannot be deleted; deactivate only supplementary rules")
	}
	var affected int64
	if used {
		tag, e := a.pool.Exec(c.Context(), `UPDATE scoring_rules SET active=FALSE, updated_by=$2, updated_at=now() WHERE id=$1`, id, currentUser(c).Email)
		err, affected = e, tag.RowsAffected()
	} else {
		tag, e := a.pool.Exec(c.Context(), `DELETE FROM scoring_rules WHERE id=$1`, id)
		err, affected = e, tag.RowsAffected()
	}
	if err != nil {
		return serverError(c, err)
	}
	if affected == 0 {
		return leadErr(c, 404, "rule not found")
	}
	auditUser(c, a.pool, "rule_deleted", "rule:"+strconv.FormatInt(id, 10), auditJSON(fiber.Map{"soft": used}))
	return c.JSON(fiber.Map{"ok": true, "soft_deleted": used})
}

// POST /scoring-rules/test: dry run, nothing saved. Body:
// {"events":[{"type":"asked_price"}], "lead":{"budget":9000}, "product_price":12900,
//
//	"rules":[optional rule objects to try instead of the saved ones]}
func (a *leadAPI) testRules(c *fiber.Ctx) error {
	var req struct {
		Events       []leadEventReq   `json:"events"`
		Lead         map[string]any   `json:"lead"`
		ProductPrice *float64         `json:"product_price"`
		Rules        []scoringRuleReq `json:"rules"`
	}
	if err := c.BodyParser(&req); err != nil {
		return leadErr(c, 400, "invalid JSON")
	}
	settings, err := a.loadSettings(c.Context())
	if err != nil {
		return serverError(c, err)
	}
	var rules []ScoringRule
	if len(req.Rules) > 0 {
		for i := range req.Rules {
			r := req.Rules[i]
			if msg := validateRuleReq(&r, true); msg != "" {
				return leadErr(c, 400, "rules["+strconv.Itoa(i)+"]: "+msg)
			}
			mode := r.Mode
			if mode == "" {
				mode = "once"
			}
			rules = append(rules, ScoringRule{ID: int64(-(i + 1)), Name: r.Name, Condition: r.Condition,
				Points: *r.Points, Mode: mode, MaxTotal: r.MaxTotal, Active: true, SortOrder: i})
		}
	} else if rules, err = a.loadRules(c.Context()); err != nil {
		return serverError(c, err)
	}
	f := &LeadFacts{Lead: req.Lead, ProductPrice: req.ProductPrice}
	if f.Lead == nil {
		f.Lead = map[string]any{}
	}
	for _, e := range req.Events {
		f.Events = append(f.Events, LeadEvent{Type: e.Type, Data: e.Data})
	}
	return c.JSON(ScoreLead(rules, settings, f))
}

func (a *leadAPI) recalcAll(c *fiber.Ctx) error {
	rows, err := a.pool.Query(c.Context(), `SELECT id FROM leads WHERE anonymized_at IS NULL`)
	if err != nil {
		return serverError(c, err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return serverError(c, err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	done := 0
	for _, id := range ids {
		if _, err := a.rescore(c.Context(), id); err == nil {
			done++
		}
	}
	auditUser(c, a.pool, "rules_recalculated", "leads", strconv.Itoa(done)+"/"+strconv.Itoa(len(ids)))
	return c.JSON(fiber.Map{"recalculated": done, "total": len(ids)})
}

// ---------- settings + keywords ----------

func (a *leadAPI) listSettings(c *fiber.Ctx) error {
	rows, err := a.pool.Query(c.Context(), `SELECT key, value FROM scoring_settings ORDER BY key`)
	if err != nil {
		return serverError(c, err)
	}
	defer rows.Close()
	out := fiber.Map{}
	for rows.Next() {
		var k string
		var v json.RawMessage
		if err := rows.Scan(&k, &v); err != nil {
			return serverError(c, err)
		}
		out[k] = v
	}
	return c.JSON(out)
}

func (a *leadAPI) patchSettings(c *fiber.Ctx) error {
	var in map[string]json.RawMessage
	if err := c.BodyParser(&in); err != nil || len(in) == 0 {
		return leadErr(c, 400, "send an object of setting keys and values")
	}
	allowed := map[string]bool{"handoff_threshold": true, "level_warm_min": true, "score_min": true, "score_max": true,
		"immediate_handoff_events": true, "disqualify_events": true, "followup_cold": true, "followup_warm": true}
	for k, v := range in {
		if !allowed[k] {
			return leadErr(c, 400, "unknown setting: "+k)
		}
		if !json.Valid(v) {
			return leadErr(c, 400, "invalid value for "+k)
		}
	}
	for k, v := range in {
		if _, err := a.pool.Exec(c.Context(), `INSERT INTO scoring_settings(key, value) VALUES($1,$2::jsonb)
			ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, k, string(v)); err != nil {
			return serverError(c, err)
		}
	}
	auditUser(c, a.pool, "scoring_settings_updated", "settings", auditJSON(in))
	return c.JSON(fiber.Map{"ok": true})
}

func (a *leadAPI) listKeywords(c *fiber.Ctx) error {
	m, err := a.loadKeywords(c.Context())
	if err != nil {
		return serverError(c, err)
	}
	return c.JSON(m)
}

// PUT replaces the whole keyword map: {"asked_price":["ราคา","เท่าไหร่"], ...}
func (a *leadAPI) replaceKeywords(c *fiber.Ctx) error {
	var in map[string][]string
	if err := c.BodyParser(&in); err != nil {
		return leadErr(c, 400, "invalid JSON")
	}
	ctx := c.Context()
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return serverError(c, err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM event_keywords`); err != nil {
		return serverError(c, err)
	}
	for ev, words := range in {
		for _, w := range words {
			if w = strings.TrimSpace(w); w != "" {
				if _, err := tx.Exec(ctx, `INSERT INTO event_keywords(event_type, keyword) VALUES($1,$2) ON CONFLICT DO NOTHING`, ev, w); err != nil {
					return serverError(c, err)
				}
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return serverError(c, err)
	}
	auditUser(c, a.pool, "event_keywords_replaced", "event_keywords", strconv.Itoa(len(in))+" events")
	return c.JSON(fiber.Map{"ok": true})
}
