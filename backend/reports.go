package main

// GET /reports/summary: numbers for the report page and the feedback loop. Wire it up in main.go after
// registerInboxRoutes:
//
//	registerReportRoutes(app, pool)
//
// Counts only (no names, no contact data). Anonymized leads are left out. A lead's outcome is its CURRENT
// status, so the report reads as "where leads stand now", not as a history.

import (
	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

func registerReportRoutes(app fiber.Router, pool *pgxpool.Pool) {
	registerCampaignProgress(app, pool) // GET /campaign-progress: where each campaign is in the loop
	app.Get("/reports/summary", func(c *fiber.Ctx) error {
		ctx := c.Context()

		// funnel by current status
		funnel := fiber.Map{"new": 0, "nurturing": 0, "qualified": 0, "handed_off": 0, "closed": 0, "lost": 0}
		rows, err := pool.Query(ctx, `SELECT status, count(*) FROM leads WHERE anonymized_at IS NULL GROUP BY status`)
		if err != nil {
			return serverError(c, err)
		}
		total := 0
		for rows.Next() {
			var s string
			var n int
			if err := rows.Scan(&s, &n); err != nil {
				rows.Close()
				return serverError(c, err)
			}
			funnel[s] = n
			total += n
		}
		rows.Close()

		// by channel and by campaign: leads, reached sales (qualified/handed_off/closed), closed, lost, average score
		group := func(sel, join, groupBy string) ([]fiber.Map, error) {
			q := `SELECT ` + sel + `, count(*),
			        count(*) FILTER (WHERE l.status IN ('qualified','handed_off','closed')),
			        count(*) FILTER (WHERE l.status='closed'),
			        count(*) FILTER (WHERE l.status='lost'),
			        COALESCE(round(avg(l.score))::int, 0)
			      FROM leads l ` + join + ` WHERE l.anonymized_at IS NULL GROUP BY ` + groupBy + ` ORDER BY count(*) DESC`
			rs, err := pool.Query(ctx, q)
			if err != nil {
				return nil, err
			}
			defer rs.Close()
			out := []fiber.Map{}
			for rs.Next() {
				var name string
				var id *int64
				var leads, reached, closed, lost, avg int
				if sel == "l.channel" {
					if err := rs.Scan(&name, &leads, &reached, &closed, &lost, &avg); err != nil {
						return nil, err
					}
					out = append(out, fiber.Map{"channel": name, "leads": leads, "reached_sales": reached, "closed": closed, "lost": lost, "avg_score": avg})
				} else {
					if err := rs.Scan(&id, &name, &leads, &reached, &closed, &lost, &avg); err != nil {
						return nil, err
					}
					out = append(out, fiber.Map{"campaign_id": id, "campaign": name, "leads": leads, "reached_sales": reached, "closed": closed, "lost": lost, "avg_score": avg})
				}
			}
			return out, rs.Err()
		}
		byChannel, err := group("l.channel", "", "l.channel")
		if err != nil {
			return serverError(c, err)
		}
		byCampaign, err := group("c.id, COALESCE(c.name, '(ไม่ระบุแคมเปญ)')", "LEFT JOIN campaigns c ON c.id = l.campaign_id", "c.id, c.name")
		if err != nil {
			return serverError(c, err)
		}

		// feedback: do higher scores really close more? Outcomes of finished leads by score band.
		bands := []fiber.Map{}
		br, err := pool.Query(ctx, `
			SELECT CASE WHEN score >= 50 THEN 'high' WHEN score >= 25 THEN 'mid' ELSE 'low' END AS band,
			       count(*) FILTER (WHERE status='closed'), count(*) FILTER (WHERE status='lost'), count(*)
			FROM leads WHERE anonymized_at IS NULL GROUP BY 1`)
		if err != nil {
			return serverError(c, err)
		}
		defer br.Close()
		seen := map[string]fiber.Map{}
		for br.Next() {
			var band string
			var closed, lost, all int
			if err := br.Scan(&band, &closed, &lost, &all); err != nil {
				return serverError(c, err)
			}
			seen[band] = fiber.Map{"band": band, "leads": all, "closed": closed, "lost": lost}
		}
		for _, b := range []string{"high", "mid", "low"} {
			if m, ok := seen[b]; ok {
				bands = append(bands, m)
			} else {
				bands = append(bands, fiber.Map{"band": b, "leads": 0, "closed": 0, "lost": 0})
			}
		}

		return c.JSON(fiber.Map{"total": total, "funnel": funnel, "by_channel": byChannel,
			"by_campaign": byCampaign, "score_bands": bands})
	})
}
