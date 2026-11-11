package main

// GET /campaigns/:id/past-results -> the summary the next AI plan is built on (also used inside planPrompt).

import (
	"context"
	"log"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

const feedbackCampaignLimit = 5

const outcomeCols = `count(*), count(*) FILTER (WHERE l.level='hot'),
	count(*) FILTER (WHERE l.status IN ('handed_off','won','closed')),
	count(*) FILTER (WHERE l.status IN ('won','closed')), count(*) FILTER (WHERE l.status='lost')`

// notTest leaves out the "mock" channel (test and demo data), so it never shapes a real plan.
const notTest = ` AND l.channel <> 'mock'`

// loadPastResults sums up the leads of earlier campaigns (never the one being planned). Counts only.
// It uses the campaigns with the same product when they have enough leads, otherwise all campaigns.
func loadPastResults(ctx context.Context, pool *pgxpool.Pool, excludeCampaign int) (pastResults, error) {
	var product *int64
	if err := pool.QueryRow(ctx, `SELECT product_id FROM campaigns WHERE id=$1`, excludeCampaign).Scan(&product); err != nil {
		product = nil // unknown campaign: fall back to the overall picture
	}
	if product != nil {
		r, err := pastResultsFor(ctx, pool, excludeCampaign, product)
		if err != nil || r.enough() {
			r.Scope = "same_product"
			return r, err
		}
	}
	r, err := pastResultsFor(ctx, pool, excludeCampaign, nil)
	r.Scope = "all_products"
	return r, err
}

// pastResultsFor aggregates the other campaigns; product != nil limits it to campaigns of that product.
func pastResultsFor(ctx context.Context, pool *pgxpool.Pool, excludeCampaign int, product *int64) (pastResults, error) {
	var r pastResults
	where := ` FROM leads l JOIN campaigns c ON c.id = l.campaign_id
		WHERE l.campaign_id <> $1 AND ($2::bigint IS NULL OR c.product_id = $2)` + notTest
	if err := pool.QueryRow(ctx, `SELECT `+outcomeCols+where, excludeCampaign, product).
		Scan(&r.Total.Leads, &r.Total.Hot, &r.Total.Handed, &r.Total.Closed, &r.Total.Lost); err != nil {
		return r, err
	}
	if !r.enough() {
		return r, nil
	}
	rows, err := pool.Query(ctx, `SELECT l.channel, `+outcomeCols+where+` GROUP BY l.channel ORDER BY count(*) DESC`, excludeCampaign, product)
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var ch pastChannel
		if err := rows.Scan(&ch.Channel, &ch.Leads, &ch.Hot, &ch.Handed, &ch.Closed, &ch.Lost); err != nil {
			rows.Close()
			return r, err
		}
		r.Channels = append(r.Channels, ch)
	}
	rows.Close()
	rows, err = pool.Query(ctx, `SELECT c.name, `+outcomeCols+where+` GROUP BY c.id, c.name ORDER BY max(c.created_at) DESC LIMIT $3`,
		excludeCampaign, product, feedbackCampaignLimit)
	if err != nil {
		return r, err
	}
	defer rows.Close()
	for rows.Next() {
		var c pastCampaign
		if err := rows.Scan(&c.Name, &c.Leads, &c.Hot, &c.Handed, &c.Closed, &c.Lost); err != nil {
			return r, err
		}
		r.Campaigns = append(r.Campaigns, c)
	}
	return r, rows.Err()
}

// withPastResults adds the earlier results to the campaign info; on a database error the plan is still made without them.
func withPastResults(ctx context.Context, pool *pgxpool.Pool, id int, ci campaignInfo) campaignInfo {
	past, err := loadPastResults(ctx, pool, id)
	if err != nil {
		log.Printf("past results: %v", err)
		return ci
	}
	ci.Past = past
	return ci
}

func registerFeedbackRoutes(app fiber.Router, pool *pgxpool.Pool) {
	app.Get("/campaigns/:id/past-results", requireRole("marketing", "admin"), func(c *fiber.Ctx) error {
		id, err := c.ParamsInt("id")
		if err != nil {
			return leadErr(c, 400, "invalid campaign id")
		}
		r, err := loadPastResults(c.UserContext(), pool, id)
		if err != nil {
			return serverError(c, err)
		}
		return c.JSON(fiber.Map{"enough": r.enough(), "min_leads": minFeedbackLeads, "summary": r.render(), "best_channel": r.best(), "scope": r.Scope,
			"total": r.Total, "channels": r.Channels, "campaigns": r.Campaigns})
	})
}
