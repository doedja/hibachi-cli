// Package tpsl watches the mark price for a symbol and fires a market close
// when the take-profit or stop-loss trigger is crossed.
package tpsl

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"strings"
	"time"

	hibachi "github.com/doedja/hibachi-go"
	"github.com/doedja/hibachi-cli/internal/journal"
	"github.com/doedja/hibachi-cli/internal/safety"
	"github.com/doedja/hibachi-cli/internal/strategies"

	"github.com/shopspring/decimal"
)

func init() {
	strategies.Register("tpsl", func() strategies.Strategy { return New() })
}

type Strategy struct {
	symbol     string
	tpSpec     string
	slSpec     string
	qtyOverrideStr string
	invocation string
}

func New() *Strategy { return &Strategy{} }

func (s *Strategy) Name() string { return "tpsl" }

func (s *Strategy) Description() string {
	return "tp/sl watcher: closes a position when mark crosses TP or SL"
}

func (s *Strategy) ParseFlags(args []string) error {
	fs := flag.NewFlagSet("tpsl", flag.ContinueOnError)
	fs.StringVar(&s.symbol, "symbol", "", "symbol, e.g. BTC/USDT-P (required)")
	fs.StringVar(&s.tpSpec, "tp", "", "take-profit: absolute price or percent e.g. 5%")
	fs.StringVar(&s.slSpec, "sl", "", "stop-loss: absolute price or percent e.g. 2%")
	fs.StringVar(&s.qtyOverrideStr, "qty", "", "qty to close (defaults to full position)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if s.symbol == "" {
		return fmt.Errorf("tpsl: --symbol is required")
	}
	if s.tpSpec == "" && s.slSpec == "" {
		return fmt.Errorf("tpsl: provide at least one of --tp or --sl")
	}
	return nil
}

func (s *Strategy) Run(ctx context.Context, deps strategies.AgentDeps) error {
	s.invocation = fmt.Sprintf("tpsl:%d", time.Now().Unix())
	log := deps.Logger
	if log == nil {
		log = func(string) {}
	}

	pos, err := findPosition(ctx, deps, s.symbol)
	if err != nil {
		return err
	}
	if pos == nil {
		return fmt.Errorf("no open position for %s", s.symbol)
	}

	entry, err := decimal.NewFromString(pos.OpenPrice)
	if err != nil {
		return fmt.Errorf("parse open price %q: %w", pos.OpenPrice, err)
	}
	posQty, err := decimal.NewFromString(pos.Quantity)
	if err != nil {
		return fmt.Errorf("parse position qty %q: %w", pos.Quantity, err)
	}
	direction := strings.ToLower(pos.Direction) // "long" or "short"

	var qty decimal.Decimal
	if s.qtyOverrideStr != "" {
		qty, err = decimal.NewFromString(s.qtyOverrideStr)
		if err != nil {
			return fmt.Errorf("parse --qty: %w", err)
		}
	} else {
		qty = posQty.Abs()
	}

	tpPrice, err := resolveTrigger(s.tpSpec, entry, direction, true)
	if err != nil {
		return fmt.Errorf("tp: %w", err)
	}
	slPrice, err := resolveTrigger(s.slSpec, entry, direction, false)
	if err != nil {
		return fmt.Errorf("sl: %w", err)
	}

	log(fmt.Sprintf("tpsl watching %s %s pos_qty=%s entry=%s tp=%s sl=%s close_qty=%s dry_run=%v",
		s.symbol, direction, posQty.String(), entry.String(),
		stringOrDash(tpPrice), stringOrDash(slPrice), qty.String(), deps.DryRun))

	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			log("tpsl stopped")
			return nil
		case <-t.C:
			price, err := deps.Client.GetPrices(ctx, s.symbol)
			if err != nil {
				log(fmt.Sprintf("get prices: %v", err))
				continue
			}
			mark, err := decimal.NewFromString(price.MarkPrice)
			if err != nil {
				log(fmt.Sprintf("parse mark: %v", err))
				continue
			}
			kind, trigger := checkTrigger(direction, mark, tpPrice, slPrice)
			if kind == "" {
				continue
			}
			return s.fireClose(ctx, deps, direction, qty, mark, kind, trigger, log)
		}
	}
}

// checkTrigger returns ("tp"|"sl"|"", triggerPrice) when the mark has crossed.
func checkTrigger(direction string, mark decimal.Decimal, tp, sl *decimal.Decimal) (string, decimal.Decimal) {
	switch direction {
	case "long":
		if tp != nil && mark.GreaterThanOrEqual(*tp) {
			return "tp", *tp
		}
		if sl != nil && mark.LessThanOrEqual(*sl) {
			return "sl", *sl
		}
	case "short":
		if tp != nil && mark.LessThanOrEqual(*tp) {
			return "tp", *tp
		}
		if sl != nil && mark.GreaterThanOrEqual(*sl) {
			return "sl", *sl
		}
	}
	return "", decimal.Zero
}

func (s *Strategy) fireClose(
	ctx context.Context, deps strategies.AgentDeps,
	direction string, qty, mark decimal.Decimal,
	kind string, trigger decimal.Decimal,
	log func(string),
) error {
	// Closing a long means SELL; closing a short means BUY.
	var side hibachi.Side
	switch direction {
	case "long":
		side = hibachi.SideAsk
	case "short":
		side = hibachi.SideBid
	default:
		return fmt.Errorf("unknown direction %q", direction)
	}

	notional, _ := qty.Mul(mark).Float64()
	if err := deps.Safety.Check(safety.Action{Kind: "trade.market", Symbol: s.symbol, NotionalUSD: notional}); err != nil {
		return fmt.Errorf("safety: %w", err)
	}

	payload := map[string]any{
		"symbol":   s.symbol,
		"trigger":  kind,
		"trigger_price": trigger.String(),
		"mark":     mark.String(),
		"qty":      qty.String(),
		"side":     string(side),
		"dry_run":  deps.DryRun,
	}
	raw, _ := json.Marshal(payload)
	evID, _ := deps.Journal.Record(ctx, journal.Event{
		Kind:    "strategy_event",
		Symbol:  s.symbol,
		Agent:   s.invocation,
		Payload: raw,
	})
	log(fmt.Sprintf("%s hit: mark=%s trigger=%s closing %s %s", strings.ToUpper(kind), mark.String(), trigger.String(), string(side), qty.String()))

	if deps.DryRun {
		log("[dry-run] no order sent")
		outcome, _ := json.Marshal(map[string]any{"status": "dry_run"})
		_ = deps.Journal.SetOutcome(ctx, evID, outcome)
		return nil
	}

	maxFees := decimal.NewFromFloat(0.0005)
	res, err := deps.Client.PlaceMarketOrder(ctx, s.symbol, side, qty, maxFees)
	if err != nil {
		outcome, _ := json.Marshal(map[string]any{"status": "error", "message": err.Error()})
		_ = deps.Journal.SetOutcome(ctx, evID, outcome)
		return fmt.Errorf("place market: %w", err)
	}
	outcome, _ := json.Marshal(map[string]any{"status": "placed", "order_id": res.OrderID})
	_ = deps.Journal.SetOutcome(ctx, evID, outcome)
	log(fmt.Sprintf("closed via market order_id=%s", res.OrderID))
	return nil
}

// resolveTrigger converts a "5%" or absolute price spec into an absolute price.
// isTP toggles how a percent moves the target (favorable vs adverse direction).
func resolveTrigger(spec string, entry decimal.Decimal, direction string, isTP bool) (*decimal.Decimal, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, nil
	}
	if strings.HasSuffix(spec, "%") {
		pctStr := strings.TrimSuffix(spec, "%")
		pct, err := decimal.NewFromString(strings.TrimSpace(pctStr))
		if err != nil {
			return nil, fmt.Errorf("invalid percent %q: %w", spec, err)
		}
		frac := pct.Div(decimal.NewFromInt(100))
		delta := entry.Mul(frac)
		var target decimal.Decimal
		switch {
		case direction == "long" && isTP:
			target = entry.Add(delta)
		case direction == "long" && !isTP:
			target = entry.Sub(delta)
		case direction == "short" && isTP:
			target = entry.Sub(delta)
		case direction == "short" && !isTP:
			target = entry.Add(delta)
		default:
			return nil, fmt.Errorf("unknown direction %q", direction)
		}
		return &target, nil
	}
	p, err := decimal.NewFromString(spec)
	if err != nil {
		return nil, fmt.Errorf("invalid price %q: %w", spec, err)
	}
	return &p, nil
}

func findPosition(ctx context.Context, deps strategies.AgentDeps, symbol string) (*hibachi.Position, error) {
	_ = ctx
	// Account snapshot lives behind the WS stream; tpsl relies on the entry
	// price, quantity, and direction that the caller just had open. Use the
	// REST pending orders + a small probe is not enough, so require the
	// caller to have an open position observable through the account client.
	// Avoid opening a WS here: the snapshot call inside cmd/account fetches
	// positions via short-lived WS already. We do the same pattern.
	info := deps.Cfg
	if info.API.APIKey == "" || info.API.AccountID == 0 {
		return nil, fmt.Errorf("tpsl needs api_key and account_id to read the current position")
	}
	// Minimal inline WS snapshot; the heavy lifting lives in cmd/account.go
	// but strategies must not depend on cmd. Defer to a subpackage-local snapshot.
	snap, err := snapshotPositions(ctx, deps)
	if err != nil {
		return nil, err
	}
	for i := range snap {
		if snap[i].Symbol == symbol {
			// Ignore closed positions: quantity zero.
			if q, err := decimal.NewFromString(snap[i].Quantity); err == nil && q.IsZero() {
				return nil, nil
			}
			return &snap[i], nil
		}
	}
	return nil, nil
}

func stringOrDash(d *decimal.Decimal) string {
	if d == nil {
		return "-"
	}
	return d.String()
}
