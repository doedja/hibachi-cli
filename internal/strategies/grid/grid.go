// Package grid runs a ladder of limit orders above and below the current mark
// and refills the opposite side when a grid level fills.
package grid

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"time"

	hibachi "github.com/doedja/hibachi-go"
	"github.com/shopspring/decimal"

	"github.com/doedja/hibachi-cli/internal/journal"
	"github.com/doedja/hibachi-cli/internal/safety"
	"github.com/doedja/hibachi-cli/internal/strategies"
)

func init() {
	strategies.Register("grid", func() strategies.Strategy { return New() })
}

type Strategy struct {
	symbol     string
	low        float64
	high       float64
	levels     int
	size       float64
	invocation string

	// Level state: price -> pending orderID (0 if unfilled slot to refill).
	// Built once on start from the evenly-spaced ladder.
	levelPrices []decimal.Decimal
	// orderLevel[orderID] = index into levelPrices; side per level stored in levelSide.
	orderLevel map[int64]int
	levelSide  []string // "BID" or "ASK"; reset when a fill happens to trigger opposite side.
}

func New() *Strategy { return &Strategy{orderLevel: map[int64]int{}} }

func (s *Strategy) Name() string { return "grid" }

func (s *Strategy) Description() string {
	return "grid: ladder of limit bids below and asks above the mark; refills on fill"
}

func (s *Strategy) ParseFlags(args []string) error {
	fs := flag.NewFlagSet("grid", flag.ContinueOnError)
	fs.StringVar(&s.symbol, "symbol", "", "symbol, e.g. BTC/USDT-P (required)")
	fs.Float64Var(&s.low, "low", 0, "lower price bound (required)")
	fs.Float64Var(&s.high, "high", 0, "upper price bound (required)")
	fs.IntVar(&s.levels, "levels", 0, "number of grid levels across the range (required)")
	fs.Float64Var(&s.size, "size", 0, "per-level order size in base units (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if s.symbol == "" {
		return fmt.Errorf("grid: --symbol is required")
	}
	if s.low <= 0 || s.high <= 0 || s.high <= s.low {
		return fmt.Errorf("grid: --low and --high must be positive with high > low")
	}
	if s.levels < 2 {
		return fmt.Errorf("grid: --levels must be at least 2")
	}
	if s.size <= 0 {
		return fmt.Errorf("grid: --size must be positive")
	}
	return nil
}

func (s *Strategy) Run(ctx context.Context, deps strategies.AgentDeps) error {
	s.invocation = fmt.Sprintf("grid:%d", time.Now().Unix())
	log := deps.Logger
	if log == nil {
		log = func(string) {}
	}
	log(fmt.Sprintf("grid starting: symbol=%s range=[%.2f, %.2f] levels=%d size=%.6f dry_run=%v",
		s.symbol, s.low, s.high, s.levels, s.size, deps.DryRun))

	// Build evenly-spaced ladder.
	lowD := decimal.NewFromFloat(s.low)
	highD := decimal.NewFromFloat(s.high)
	step := highD.Sub(lowD).Div(decimal.NewFromInt(int64(s.levels - 1)))
	s.levelPrices = make([]decimal.Decimal, s.levels)
	s.levelSide = make([]string, s.levels)
	for i := 0; i < s.levels; i++ {
		s.levelPrices[i] = lowD.Add(step.Mul(decimal.NewFromInt(int64(i))))
	}

	price, err := deps.Client.GetPrices(ctx, s.symbol)
	if err != nil {
		return fmt.Errorf("get prices: %w", err)
	}
	mark, err := decimal.NewFromString(price.MarkPrice)
	if err != nil {
		return fmt.Errorf("parse mark: %w", err)
	}

	// Classify each level as bid (below mark) or ask (above mark); skip the
	// closest level to avoid crossing.
	for i, p := range s.levelPrices {
		if p.LessThan(mark) {
			s.levelSide[i] = "BID"
		} else if p.GreaterThan(mark) {
			s.levelSide[i] = "ASK"
		} else {
			s.levelSide[i] = ""
		}
	}

	if err := s.placeAllOpenLevels(ctx, deps, log); err != nil {
		return fmt.Errorf("initial placement: %w", err)
	}

	// Poll for fills every 10s. On exit, print summary and return.
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			s.printSummary(log)
			return nil
		case <-t.C:
			if err := s.pollAndRefill(ctx, deps, log); err != nil {
				log(fmt.Sprintf("poll error: %v", err))
			}
		}
	}
}

// placeAllOpenLevels places a limit order at each level that currently has no
// live order associated with it.
func (s *Strategy) placeAllOpenLevels(ctx context.Context, deps strategies.AgentDeps, log func(string)) error {
	taken := map[int]bool{}
	for _, idx := range s.orderLevel {
		taken[idx] = true
	}
	for i, side := range s.levelSide {
		if side == "" || taken[i] {
			continue
		}
		if err := s.placeLevel(ctx, deps, i, log); err != nil {
			log(fmt.Sprintf("place level %d: %v", i, err))
		}
	}
	return nil
}

func (s *Strategy) placeLevel(ctx context.Context, deps strategies.AgentDeps, idx int, log func(string)) error {
	priceD := s.levelPrices[idx]
	sideStr := s.levelSide[idx]
	if sideStr == "" {
		return nil
	}
	var side hibachi.Side
	switch sideStr {
	case "BID":
		side = hibachi.SideBid
	case "ASK":
		side = hibachi.SideAsk
	default:
		return fmt.Errorf("invalid side %q", sideStr)
	}
	qty := decimal.NewFromFloat(s.size)
	notionalF, _ := qty.Mul(priceD).Float64()
	if err := deps.Safety.Check(safety.Action{Kind: "trade.limit", Symbol: s.symbol, NotionalUSD: notionalF}); err != nil {
		return fmt.Errorf("safety: %w", err)
	}

	payload := map[string]any{
		"level":    idx,
		"side":     sideStr,
		"price":    priceD.String(),
		"qty":      qty.String(),
		"notional": notionalF,
		"dry_run":  deps.DryRun,
	}
	raw, _ := json.Marshal(payload)
	evID, _ := deps.Journal.Record(ctx, journal.Event{
		Kind:    "strategy_event",
		Symbol:  s.symbol,
		Agent:   s.invocation,
		Payload: raw,
	})

	if deps.DryRun {
		log(fmt.Sprintf("[dry-run] would place %s %s @ %s (level %d)", sideStr, qty.String(), priceD.String(), idx))
		outcome, _ := json.Marshal(map[string]any{"status": "dry_run"})
		_ = deps.Journal.SetOutcome(ctx, evID, outcome)
		return nil
	}

	maxFees := decimal.NewFromFloat(0.0005)
	res, err := deps.Client.PlaceLimitOrder(ctx, s.symbol, side, qty, priceD, maxFees)
	if err != nil {
		outcome, _ := json.Marshal(map[string]any{"status": "error", "message": err.Error()})
		_ = deps.Journal.SetOutcome(ctx, evID, outcome)
		return fmt.Errorf("place limit: %w", err)
	}
	var oid int64
	fmt.Sscanf(res.OrderID, "%d", &oid)
	if oid > 0 {
		s.orderLevel[oid] = idx
	}
	outcome, _ := json.Marshal(map[string]any{"status": "placed", "order_id": res.OrderID})
	_ = deps.Journal.SetOutcome(ctx, evID, outcome)
	log(fmt.Sprintf("placed %s %s @ %s (level %d, order_id=%s)", sideStr, qty.String(), priceD.String(), idx, res.OrderID))
	return nil
}

// pollAndRefill fetches pending orders, detects fills (orders that vanished),
// flips the side on the filled level, and places a new order on the opposite
// side at the adjacent level.
func (s *Strategy) pollAndRefill(ctx context.Context, deps strategies.AgentDeps, log func(string)) error {
	if deps.DryRun {
		return nil
	}
	open, err := deps.Client.GetPendingOrders(ctx)
	if err != nil {
		return fmt.Errorf("pending orders: %w", err)
	}
	alive := map[int64]bool{}
	for _, o := range open {
		alive[o.OrderID] = true
	}
	var filled []int // level indexes
	for oid, idx := range s.orderLevel {
		if !alive[oid] {
			filled = append(filled, idx)
			delete(s.orderLevel, oid)
		}
	}
	if len(filled) == 0 {
		return nil
	}
	for _, idx := range filled {
		prevSide := s.levelSide[idx]
		payload := map[string]any{
			"level": idx,
			"filled_side": prevSide,
			"price": s.levelPrices[idx].String(),
		}
		raw, _ := json.Marshal(payload)
		_, _ = deps.Journal.Record(ctx, journal.Event{
			Kind:    "strategy_event",
			Symbol:  s.symbol,
			Agent:   s.invocation,
			Payload: raw,
		})
		log(fmt.Sprintf("fill detected at level %d (%s %s)", idx, prevSide, s.levelPrices[idx].String()))

		// Flip side for that level so the next placement sits on the other side.
		switch prevSide {
		case "BID":
			s.levelSide[idx] = "ASK"
		case "ASK":
			s.levelSide[idx] = "BID"
		}
		// Also refill the adjacent opposite-side level to keep the ladder alive.
		adj := -1
		if prevSide == "BID" && idx+1 < len(s.levelSide) {
			adj = idx + 1
		}
		if prevSide == "ASK" && idx-1 >= 0 {
			adj = idx - 1
		}
		if err := s.placeLevel(ctx, deps, idx, log); err != nil {
			log(fmt.Sprintf("replace level %d: %v", idx, err))
		}
		if adj >= 0 {
			taken := false
			for _, v := range s.orderLevel {
				if v == adj {
					taken = true
					break
				}
			}
			if !taken && s.levelSide[adj] != "" {
				if err := s.placeLevel(ctx, deps, adj, log); err != nil {
					log(fmt.Sprintf("refill adjacent %d: %v", adj, err))
				}
			}
		}
	}
	return nil
}

func (s *Strategy) printSummary(log func(string)) {
	log(fmt.Sprintf("grid stopped. %d orders still live; use 'hibachi trade cancel-all' to clear.", len(s.orderLevel)))
	for oid, idx := range s.orderLevel {
		log(fmt.Sprintf("  order_id=%d level=%d side=%s price=%s", oid, idx, s.levelSide[idx], s.levelPrices[idx].String()))
	}
}
