package main

// Rule-based lead scoring engine (pure functions: no DB, no HTTP).
// Conditions are stored as JSON and interpreted here; nothing is ever eval'd.

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

const maxCondDepth = 5

var allowedOps = map[string]bool{"=": true, "!=": true, ">": true, ">=": true, "<": true, "<=": true, "in": true, "contains": true}
var allowedRefs = map[string]bool{"product.price": true}

// CondPred compares a field with a value. Value is a literal, an array (for "in"),
// or a reference object {"ref":"product.price","multiply":0.7}.
type CondPred struct {
	Field string          `json:"field"`
	Op    string          `json:"op"`
	Value json.RawMessage `json:"value"`
}

// ScoringCond is exactly one of: event (+where), lead, all, any, not.
type ScoringCond struct {
	Event string        `json:"event,omitempty"`
	Where *CondPred     `json:"where,omitempty"`
	Lead  *CondPred     `json:"lead,omitempty"`
	All   []ScoringCond `json:"all,omitempty"`
	Any   []ScoringCond `json:"any,omitempty"`
	Not   *ScoringCond  `json:"not,omitempty"`
}

type valueRef struct {
	Ref      string   `json:"ref"`
	Multiply *float64 `json:"multiply,omitempty"`
}

// ParseCondition parses and validates a JSON condition (use before saving a rule).
func ParseCondition(raw []byte) (*ScoringCond, error) {
	var c ScoringCond
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("condition is not valid JSON: %w", err)
	}
	if err := validateCond(&c, 1); err != nil {
		return nil, err
	}
	return &c, nil
}

func validateCond(c *ScoringCond, depth int) error {
	if depth > maxCondDepth {
		return fmt.Errorf("condition nested deeper than %d levels", maxCondDepth)
	}
	kinds := 0
	if c.Event != "" {
		kinds++
	}
	if c.Lead != nil {
		kinds++
	}
	if len(c.All) > 0 {
		kinds++
	}
	if len(c.Any) > 0 {
		kinds++
	}
	if c.Not != nil {
		kinds++
	}
	if kinds != 1 {
		return errors.New("condition must contain exactly one of: event, lead, all, any, not")
	}
	if c.Where != nil && c.Event == "" {
		return errors.New("'where' can only be used together with 'event'")
	}
	if c.Where != nil {
		if err := validatePred(c.Where); err != nil {
			return err
		}
	}
	if c.Lead != nil {
		if err := validatePred(c.Lead); err != nil {
			return err
		}
	}
	for i := range c.All {
		if err := validateCond(&c.All[i], depth+1); err != nil {
			return err
		}
	}
	for i := range c.Any {
		if err := validateCond(&c.Any[i], depth+1); err != nil {
			return err
		}
	}
	if c.Not != nil {
		return validateCond(c.Not, depth+1)
	}
	return nil
}

func validatePred(p *CondPred) error {
	if strings.TrimSpace(p.Field) == "" {
		return errors.New("predicate needs a 'field'")
	}
	if !allowedOps[p.Op] {
		return fmt.Errorf("unsupported operator %q", p.Op)
	}
	if len(p.Value) == 0 {
		return errors.New("predicate needs a 'value'")
	}
	var ref valueRef
	if json.Unmarshal(p.Value, &ref) == nil && ref.Ref != "" && !allowedRefs[ref.Ref] {
		return fmt.Errorf("unknown reference %q", ref.Ref)
	}
	return nil
}

// ---- evaluation ----

type LeadEvent struct {
	Type string
	Data map[string]any
}

// LeadFacts is everything a rule may look at for one lead.
type LeadFacts struct {
	Lead         map[string]any // e.g. {"budget": 12000.0, "channel": "line"}
	ProductPrice *float64       // price of the campaign's product, if any
	Events       []LeadEvent
}

// eval returns how many times the condition matched (0 = no match).
func evalCondition(c *ScoringCond, f *LeadFacts) int {
	switch {
	case c.Event != "":
		n := 0
		for _, e := range f.Events {
			if e.Type != c.Event {
				continue
			}
			if c.Where == nil || comparePred(c.Where, e.Data, f) {
				n++
			}
		}
		return n
	case c.Lead != nil:
		if comparePred(c.Lead, f.Lead, f) {
			return 1
		}
		return 0
	case len(c.All) > 0:
		min := math.MaxInt32
		for i := range c.All {
			n := evalCondition(&c.All[i], f)
			if n == 0 {
				return 0
			}
			if n < min {
				min = n
			}
		}
		return min
	case len(c.Any) > 0:
		max := 0
		for i := range c.Any {
			if n := evalCondition(&c.Any[i], f); n > max {
				max = n
			}
		}
		return max
	case c.Not != nil:
		if evalCondition(c.Not, f) == 0 {
			return 1
		}
	}
	return 0
}

func scoreResolveValue(raw json.RawMessage, f *LeadFacts) (any, bool) {
	var ref valueRef
	if json.Unmarshal(raw, &ref) == nil && ref.Ref != "" {
		if ref.Ref == "product.price" {
			if f.ProductPrice == nil {
				return nil, false
			}
			v := *f.ProductPrice
			if ref.Multiply != nil {
				v *= *ref.Multiply
			}
			return v, true
		}
		return nil, false
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil, false
	}
	return v, true
}

func scoreToFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		n, err := x.Float64()
		return n, err == nil
	}
	return 0, false
}

func comparePred(p *CondPred, data map[string]any, f *LeadFacts) bool {
	left, ok := data[p.Field]
	if !ok || left == nil {
		return false
	}
	right, ok := scoreResolveValue(p.Value, f)
	if !ok {
		return false
	}
	switch p.Op {
	case "in":
		arr, ok := right.([]any)
		if !ok {
			return false
		}
		for _, item := range arr {
			if scoreLooselyEqual(left, item) {
				return true
			}
		}
		return false
	case "contains":
		if s, ok := left.(string); ok {
			sub, ok := right.(string)
			return ok && strings.Contains(strings.ToLower(s), strings.ToLower(sub))
		}
		if arr, ok := left.([]any); ok {
			for _, item := range arr {
				if scoreLooselyEqual(item, right) {
					return true
				}
			}
		}
		return false
	case "=":
		return scoreLooselyEqual(left, right)
	case "!=":
		return !scoreLooselyEqual(left, right)
	}
	l, lok := scoreToFloat(left)
	r, rok := scoreToFloat(right)
	if !lok || !rok {
		return false
	}
	switch p.Op {
	case ">":
		return l > r
	case ">=":
		return l >= r
	case "<":
		return l < r
	case "<=":
		return l <= r
	}
	return false
}

func scoreLooselyEqual(a, b any) bool {
	if x, ok := scoreToFloat(a); ok {
		if y, ok := scoreToFloat(b); ok {
			return x == y
		}
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}

// ---- scoring ----

type ScoringRule struct {
	ID        int64
	Name      string
	Condition json.RawMessage
	Points    int
	Mode      string // "once" | "each"
	MaxTotal  *int   // cap on the TOTAL this rule can add (magnitude; sign follows Points)
	Active    bool
	SortOrder int
}

type ScoringSettings struct {
	HandoffThreshold int
	WarmMin          int
	ScoreMin         int
	ScoreMax         int
	ImmediateHandoff []string // event types that hand off at once
	Disqualify       []string // event types that stop all follow-up
}

func DefaultScoringSettings() ScoringSettings {
	return ScoringSettings{
		HandoffThreshold: 50, WarmMin: 25, ScoreMin: 0, ScoreMax: 100,
		ImmediateHandoff: []string{"requested_visit", "requested_human", "requested_callback"},
		Disqualify:       []string{"unsubscribed"},
	}
}

type AppliedRule struct {
	RuleID   int64  `json:"rule_id"`
	RuleName string `json:"rule_name"`
	Matches  int    `json:"matches"`
	Points   int    `json:"points"`
}

type ScoreResult struct {
	Score            int           `json:"score"`
	Level            string        `json:"level"` // cold | warm | hot | disqualified
	Qualified        bool          `json:"qualified"`
	ImmediateHandoff bool          `json:"immediate_handoff"`
	Disqualified     bool          `json:"disqualified"`
	Breakdown        []AppliedRule `json:"breakdown"`
	Skipped          []string      `json:"skipped_rules,omitempty"` // rules with a bad condition
}

func hasAnyEvent(events []LeadEvent, types []string) bool {
	for _, e := range events {
		for _, t := range types {
			if e.Type == t {
				return true
			}
		}
	}
	return false
}

// Score evaluates all active rules for one lead.
func ScoreLead(rules []ScoringRule, s ScoringSettings, f *LeadFacts) ScoreResult {
	sorted := append([]ScoringRule(nil), rules...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].SortOrder < sorted[j].SortOrder })

	res := ScoreResult{Breakdown: []AppliedRule{}}
	total := 0
	for _, r := range sorted {
		if !r.Active {
			continue
		}
		cond, err := ParseCondition(r.Condition)
		if err != nil {
			res.Skipped = append(res.Skipped, r.Name)
			continue
		}
		n := evalCondition(cond, f)
		if n == 0 {
			continue
		}
		if r.Mode != "each" {
			n = 1
		}
		pts := r.Points * n
		if r.MaxTotal != nil {
			limit := *r.MaxTotal
			if limit < 0 {
				limit = -limit
			}
			if r.Points >= 0 && pts > limit {
				pts = limit
			}
			if r.Points < 0 && pts < -limit {
				pts = -limit
			}
		}
		total += pts
		res.Breakdown = append(res.Breakdown, AppliedRule{RuleID: r.ID, RuleName: r.Name, Matches: n, Points: pts})
	}
	if total < s.ScoreMin {
		total = s.ScoreMin
	}
	if total > s.ScoreMax {
		total = s.ScoreMax
	}
	res.Score = total

	switch {
	case hasAnyEvent(f.Events, s.Disqualify):
		res.Disqualified, res.Level = true, "disqualified"
	case hasAnyEvent(f.Events, s.ImmediateHandoff):
		res.ImmediateHandoff, res.Qualified, res.Level = true, true, "hot"
	case total >= s.HandoffThreshold:
		res.Qualified, res.Level = true, "hot"
	case total >= s.WarmMin:
		res.Level = "warm"
	default:
		res.Level = "cold"
	}
	return res
}

// ---- keyword detection (rules first, AI later) ----

// DetectEvents maps a customer message to event types using keyword lists.
// Thai has no word spaces, so this is a case-insensitive substring match.
func DetectEvents(text string, keywords map[string][]string) []string {
	t := strings.ToLower(text)
	var out []string
	for event, words := range keywords {
		for _, w := range words {
			if w != "" && strings.Contains(t, strings.ToLower(w)) {
				out = append(out, event)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}
