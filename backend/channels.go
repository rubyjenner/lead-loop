package main

// Chat channels: customer messages come in here. The mock channel is first; LINE will reuse the same core.
//
// Wire it up in main.go right after registerLeadRoutes:
//
//	registerChannelRoutes(app, pool)
//
// PDPA rule: before a person has given consent NOTHING about them is stored, not even their channel id.
// An unknown (or withdrawn) person only gets the consent prompt back; the message text is never read.
// The text of known customers is used only to detect events and is never stored.

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var errChatCampaignNotFound = errors.New("campaign not found")

const (
	chatMockChannel    = "mock"
	chatMaxExternalID  = 100
	chatMaxMessageText = 2000
)

type chatChannelAPI struct{ leads *leadAPI }

func registerChannelRoutes(app fiber.Router, pool *pgxpool.Pool) {
	ch := &chatChannelAPI{leads: &leadAPI{pool: pool}}
	mk := requireRole("marketing", "admin")
	app.Post("/channels/mock/messages", mk, ch.mockMessage)
	app.Post("/channels/mock/consent", mk, ch.mockConsent)
}

func chatConsentVersion() string {
	if v := strings.TrimSpace(os.Getenv("CONSENT_VERSION")); v != "" {
		return v
	}
	return "v1"
}

// ---------- core (no HTTP: LINE will call these too) ----------

type chatIncomingResult struct {
	NeedsConsent     bool     `json:"needs_consent"`
	ConsentVersion   string   `json:"consent_version,omitempty"`
	LeadID           int64    `json:"lead_id,omitempty"`
	Reply            string   `json:"reply"`
	Detected         []string `json:"detected"`
	Score            int      `json:"score"`
	Level            string   `json:"level"`
	Qualified        bool     `json:"qualified"`
	ImmediateHandoff bool     `json:"immediate_handoff"`
}

func (ch *chatChannelAPI) findLead(ctx context.Context, channel, extID string) (int64, bool, error) {
	var id int64
	err := ch.leads.pool.QueryRow(ctx,
		`SELECT id FROM leads WHERE channel=$1 AND channel_user_id=$2 AND anonymized_at IS NULL ORDER BY id DESC LIMIT 1`,
		channel, extID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

func (ch *chatChannelAPI) loadProduct(ctx context.Context, leadID int64) (*chatProduct, error) {
	var name, desc *string
	var price *float64
	err := ch.leads.pool.QueryRow(ctx, `
		SELECT p.name, p.description, p.price::float8
		FROM leads l JOIN campaigns c ON c.id = l.campaign_id JOIN products p ON p.id = c.product_id
		WHERE l.id = $1`, leadID).Scan(&name, &desc, &price)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p := &chatProduct{Price: price}
	if name != nil {
		p.Name = *name
	}
	if desc != nil {
		p.Description = *desc
	}
	return p, nil
}

func (ch *chatChannelAPI) handleIncoming(ctx context.Context, channel, extID, text string) (chatIncomingResult, error) {
	id, found, err := ch.findLead(ctx, channel, extID)
	if err != nil {
		return chatIncomingResult{}, err
	}
	if found {
		if found, err = ch.leads.leadAllowsProcessing(ctx, id); err != nil {
			return chatIncomingResult{}, err
		}
	}
	if !found { // unknown or consent withdrawn: ask for consent, store nothing, do not read the text
		return chatIncomingResult{
			NeedsConsent:   true,
			ConsentVersion: chatConsentVersion(),
			Reply:          ChatConsentPrompt(os.Getenv("PRIVACY_POLICY_URL"), os.Getenv("PDPA_RETENTION_TEXT")),
			Detected:       []string{},
		}, nil
	}

	kw, err := ch.leads.loadKeywords(ctx)
	if err != nil {
		return chatIncomingResult{}, err
	}
	out := chatIncomingResult{LeadID: id, Detected: []string{}}
	if types := DetectEvents(text, kw); len(types) > 0 {
		evs := make([]leadEventReq, 0, len(types))
		for _, t := range types {
			evs = append(evs, leadEventReq{Type: t})
		}
		detected, res, err := ch.leads.applyEvents(ctx, id, evs)
		if err != nil {
			return chatIncomingResult{}, err
		}
		out.Detected, out.Score, out.Level = detected, res.Score, res.Level
		out.Qualified, out.ImmediateHandoff = res.Qualified, res.ImmediateHandoff
	} else { // nothing detected: do not recalculate, just report the current score
		if err := ch.leads.pool.QueryRow(ctx, `SELECT score, level FROM leads WHERE id=$1`, id).Scan(&out.Score, &out.Level); err != nil {
			return chatIncomingResult{}, err
		}
	}
	product, err := ch.loadProduct(ctx, id)
	if err != nil {
		return chatIncomingResult{}, err
	}
	out.Reply = ComposeChatReply(out.Detected, product)
	return out, nil
}

// grantConsent creates the lead (first consent) or renews consent (returning customer).
// created=false means an existing lead was updated.
func (ch *chatChannelAPI) grantConsent(ctx context.Context, channel, extID, displayName string, campaignID *int64,
	marketing bool, version, createdBy string) (int64, bool, error) {
	if campaignID != nil {
		var ok bool
		if err := ch.leads.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM campaigns WHERE id=$1)`, *campaignID).Scan(&ok); err != nil {
			return 0, false, err
		}
		if !ok {
			return 0, false, errChatCampaignNotFound
		}
	}
	for attempt := 0; attempt < 2; attempt++ { // 2nd round only if two requests raced to create the same person
		id, found, err := ch.findLead(ctx, channel, extID)
		if err != nil {
			return 0, false, err
		}
		if found {
			return id, false, ch.renewConsent(ctx, id, marketing, version)
		}
		id, err = ch.insertLead(ctx, channel, extID, displayName, campaignID, marketing, version, createdBy)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique violation: someone just created it
			continue
		}
		if err != nil {
			return 0, false, err
		}
		if _, err := ch.leads.rescore(ctx, id); err != nil {
			return 0, false, err
		}
		return id, true, nil
	}
	return 0, false, errors.New("could not create lead")
}

func (ch *chatChannelAPI) insertLead(ctx context.Context, channel, extID, displayName string, campaignID *int64,
	marketing bool, version, createdBy string) (int64, error) {
	tx, err := ch.leads.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var id int64
	err = tx.QueryRow(ctx, `
		INSERT INTO leads(campaign_id, display_name, channel, channel_user_id,
		  consent_general, consent_marketing, consent_version, consent_at, created_by)
		VALUES($1,$2,$3,$4,TRUE,$5,$6,now(),$7) RETURNING id`,
		campaignID, displayName, channel, extID, marketing, version, createdBy).Scan(&id)
	if err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO consent_events(lead_id, kind, action, text_version) VALUES($1,'general','granted',$2)`, id, version); err != nil {
		return 0, err
	}
	if marketing {
		if _, err = tx.Exec(ctx, `INSERT INTO consent_events(lead_id, kind, action, text_version) VALUES($1,'marketing','granted',$2)`, id, version); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit(ctx)
}

func (ch *chatChannelAPI) renewConsent(ctx context.Context, id int64, marketing bool, version string) error {
	tx, err := ch.leads.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var valid, curMarketing bool
	if err := tx.QueryRow(ctx, `SELECT consent_general AND consent_withdrawn_at IS NULL, consent_marketing
		FROM leads WHERE id=$1 FOR UPDATE`, id).Scan(&valid, &curMarketing); err != nil {
		return err
	}
	action := func(b bool) string {
		if b {
			return "granted"
		}
		return "withdrawn"
	}
	if valid {
		if marketing == curMarketing {
			return nil // nothing changed
		}
		if _, err := tx.Exec(ctx, `UPDATE leads SET consent_marketing=$2, updated_at=now() WHERE id=$1`, id, marketing); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO consent_events(lead_id, kind, action, text_version) VALUES($1,'marketing',$2,$3)`, id, action(marketing), version); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `UPDATE leads SET consent_general=TRUE, consent_marketing=$2, consent_withdrawn_at=NULL,
		consent_version=$3, consent_at=now(), updated_at=now() WHERE id=$1`, id, marketing, version); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO consent_events(lead_id, kind, action, text_version) VALUES($1,'general','granted',$2)`, id, version); err != nil {
		return err
	}
	if marketing {
		if _, err := tx.Exec(ctx, `INSERT INTO consent_events(lead_id, kind, action, text_version) VALUES($1,'marketing','granted',$2)`, id, version); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ---------- mock channel HTTP ----------

// POST /channels/mock/messages {"external_id":"cust-1","text":"..."}
func (ch *chatChannelAPI) mockMessage(c *fiber.Ctx) error {
	var req struct {
		ExternalID string `json:"external_id"`
		Text       string `json:"text"`
	}
	if err := c.BodyParser(&req); err != nil {
		return leadErr(c, 400, "invalid JSON")
	}
	req.ExternalID = strings.TrimSpace(req.ExternalID)
	if req.ExternalID == "" || len(req.ExternalID) > chatMaxExternalID || strings.TrimSpace(req.Text) == "" {
		return leadErr(c, 400, "external_id (up to 100 chars) and text are required")
	}
	if len([]rune(req.Text)) > chatMaxMessageText {
		return leadErr(c, 400, "text is too long")
	}
	res, err := ch.handleIncoming(c.Context(), chatMockChannel, req.ExternalID, req.Text)
	if err != nil {
		return serverError(c, err)
	}
	return c.JSON(res)
}

// POST /channels/mock/consent {"external_id":"cust-1","display_name":"...","campaign_id":1,"consent_marketing":false,"consent_version":"v1"}
// This is the "customer pressed accept" step. Calling it again for the same person never creates a second lead.
func (ch *chatChannelAPI) mockConsent(c *fiber.Ctx) error {
	var req struct {
		ExternalID       string `json:"external_id"`
		DisplayName      string `json:"display_name"`
		CampaignID       *int64 `json:"campaign_id"`
		ConsentMarketing bool   `json:"consent_marketing"`
		ConsentVersion   string `json:"consent_version"`
	}
	if err := c.BodyParser(&req); err != nil {
		return leadErr(c, 400, "invalid JSON")
	}
	req.ExternalID = strings.TrimSpace(req.ExternalID)
	if req.ExternalID == "" || len(req.ExternalID) > chatMaxExternalID {
		return leadErr(c, 400, "external_id (up to 100 chars) is required")
	}
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	if req.DisplayName == "" {
		req.DisplayName = "ลูกค้า (mock)"
	}
	if strings.TrimSpace(req.ConsentVersion) == "" {
		req.ConsentVersion = chatConsentVersion()
	}
	id, created, err := ch.grantConsent(c.Context(), chatMockChannel, req.ExternalID, req.DisplayName, req.CampaignID,
		req.ConsentMarketing, req.ConsentVersion, currentUser(c).Email)
	if errors.Is(err, errChatCampaignNotFound) {
		return leadErr(c, 404, "campaign not found")
	}
	if err != nil {
		return serverError(c, err)
	}
	action := "lead_consent_granted"
	if created {
		action = "lead_created"
	}
	auditUser(c, ch.leads.pool, action, "lead:"+strconv.FormatInt(id, 10), "via mock channel")
	return c.JSON(fiber.Map{"lead_id": id, "created": created,
		"reply": "ขอบคุณที่ยินยอมค่ะ ถามราคาหรือรายละเอียดสินค้าได้เลย"})
}
