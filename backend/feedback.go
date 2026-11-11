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

// loadPastResults sums up the leads of every campaign except the one being planned. Counts only.
func loadPastResults(ctx context.Context, pool *pgxpool.Pool, excludeCampaign int) (pastResults, error) {
	var r pastResults
	if err := pool.QueryRow(ctx, `SELECT `+outcomeCols+` FROM leads l WHERE l.campaign_id IS NOT NULL AND l.campaign_id <> $1`, excludeCampaign).
		Scan(&r.Total.Leads, &r.Total.Hot, &r.Total.Handed, &r.Total.Closed, &r.Total.Lost); err != nil {
		return r, err
	}
	if !r.enough() {
		return r, nil
	}
	rows, err := pool.Query(ctx, `SELECT l.channel, `+outcomeCols+` FROM leads l
		WHERE l.campaign_id IS NOT NULL AND l.campaign_id <> $1 GROUP BY l.channel ORDER BY count(*) DESC`, excludeCampaign)
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
	rows, err = pool.Query(ctx, `SELECT c.name, `+outcomeCols+` FROM leads l JOIN campaigns c ON c.id=l.campaign_id
		WHERE l.campaign_id <> $1 GROUP BY c.id, c.name ORDER BY max(c.created_at) DESC LIMIT $2`, excludeCampaign, feedbackCampaignLimit)
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
		return c.JSON(fiber.Map{"enough": r.enough(), "min_leads": minFeedbackLeads, "summary": r.render(), "best_channel": r.best(),
			"total": r.Total, "channels": r.Channels, "campaigns": r.Campaigns})
	})
}
