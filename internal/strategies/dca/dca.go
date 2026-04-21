// Package dca runs periodic market buys sized in USD notional.
package dca

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
	strategies.Register("dca", func() strategies.Strategy { return New() })
}

type Strategy struct {
	symbol     string
	sizeUSD    float64
	interval   time.Duration
	totalCap   float64
	spentUSD   float64
	invocation string
}

func New() *Strategy { return &Strategy{} }

func (s *Strategy) Name() string { return "dca" }

func (s *Strategy) Description() string {
	return "dollar-cost averaging: market-buy a fixed USD size every interval"
}

func (s *Strategy) ParseFlags(args []string) error {
	fs := flag.NewFlagSet("dca", flag.ContinueOnError)
	var intervalStr string
	fs.StringVar(&s.symbol, "symbol", "", "symbol to buy, e.g. BTC/USDT-P (required)")
	fs.Float64Var(&s.sizeUSD, "size", 0, "USD notional per buy (required)")
	fs.StringVar(&intervalStr, "interval", "1h", "time between buys (Go duration)")
	fs.Float64Var(&s.totalCap, "total", 0, "optional USD cap; stop after spending this much")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if s.symbol == "" {
		return fmt.Errorf("dca: --symbol is required")
	}
	if s.sizeUSD <= 0 {
		return fmt.Errorf("dca: --size must be positive")
	}
	d, err := time.ParseDuration(intervalStr)
	if err != nil {
		return fmt.Errorf("dca: invalid --interval: %w", err)
	}
	if d <= 0 {
		return fmt.Errorf("dca: --interval must be positive")
	}
	s.interval = d
	return nil
}

func (s *Strategy) Run(ctx context.Context, deps strategies.AgentDeps) error {
	s.invocation = fmt.Sprintf("dca:%d", time.Now().Unix())
	log := deps.Logger
	if log == nil {
		log = func(string) {}
	}
	log(fmt.Sprintf("dca running: symbol=%s size=$%.2f interval=%s cap=$%.2f dry_run=%v",
		s.symbol, s.sizeUSD, s.interval, s.totalCap, deps.DryRun))

	// Fire once immediately, then on the interval tick.
	if err := s.tick(ctx, deps, log); err != nil {
		log(fmt.Sprintf("first tick error: %v", err))
	}
	t := time.NewTicker(s.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			log("dca stopped")
			return nil
		case <-t.C:
			if err := s.tick(ctx, deps, log); err != nil {
				log(fmt.Sprintf("tick error: %v", err))
			}
		}
	}
}

func (s *Strategy) tick(ctx context.Context, deps strategies.AgentDeps, log func(string)) error {
	if s.totalCap > 0 && s.spentUSD >= s.totalCap {
		log(fmt.Sprintf("dca cap reached: spent=$%.2f cap=$%.2f", s.spentUSD, s.totalCap))
		return nil
	}

	price, err := deps.Client.GetPrices(ctx, s.symbol)
	if err != nil {
		return fmt.Errorf("get prices: %w", err)
	}
	mark, err := decimal.NewFromString(price.MarkPrice)
	if err != nil {
		return fmt.Errorf("parse mark price %q: %w", price.MarkPrice, err)
	}
	if mark.IsZero() {
		return fmt.Errorf("mark price is zero; skipping")
	}

	sizeDec := decimal.NewFromFloat(s.sizeUSD)
	qty := sizeDec.Div(mark)

	if err := deps.Safety.Check(safety.Action{Kind: "trade.market", Symbol: s.symbol, NotionalUSD: s.sizeUSD}); err != nil {
		return fmt.Errorf("safety: %w", err)
	}

	payload := map[string]any{
		"symbol":   s.symbol,
		"size_usd": s.sizeUSD,
		"price":    price.MarkPrice,
		"qty":      qty.String(),
		"dry_run":  deps.DryRun,
	}
	raw, _ := json.Marshal(payload)
	evID, jerr := deps.Journal.Record(ctx, journal.Event{
		Kind:    "strategy_event",
		Symbol:  s.symbol,
		Agent:   s.invocation,
		Payload: raw,
	})
	if jerr != nil {
		log(fmt.Sprintf("journal record: %v", jerr))
	}

	if deps.DryRun {
		log(fmt.Sprintf("[dry-run] would buy %s of %s at ~%s (size=$%.2f)",
			qty.String(), s.symbol, price.MarkPrice, s.sizeUSD))
		outcome, _ := json.Marshal(map[string]any{"status": "dry_run"})
		_ = deps.Journal.SetOutcome(ctx, evID, outcome)
		s.spentUSD += s.sizeUSD
		return nil
	}

	maxFees := decimal.NewFromFloat(0.0005)
	res, err := deps.Client.PlaceMarketOrder(ctx, s.symbol, hibachi.SideBid, qty, maxFees)
	if err != nil {
		outcome, _ := json.Marshal(map[string]any{"status": "error", "message": err.Error()})
		_ = deps.Journal.SetOutcome(ctx, evID, outcome)
		return fmt.Errorf("place market: %w", err)
	}
	outcome, _ := json.Marshal(map[string]any{
		"status":   "placed",
		"order_id": res.OrderID,
		"nonce":    res.Nonce,
	})
	_ = deps.Journal.SetOutcome(ctx, evID, outcome)
	s.spentUSD += s.sizeUSD
	log(fmt.Sprintf("bought %s %s at ~%s (order_id=%s spent=$%.2f)",
		qty.String(), s.symbol, price.MarkPrice, res.OrderID, s.spentUSD))
	return nil
}
