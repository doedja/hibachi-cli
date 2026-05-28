package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	hibachi "github.com/doedja/hibachi-go"

	"github.com/doedja/hibachi-cli/internal/app"
	"github.com/doedja/hibachi-cli/internal/journal"
	"github.com/doedja/hibachi-cli/internal/output"
	"github.com/doedja/hibachi-cli/internal/safety"
)

func newTradeCmd() *cobra.Command {
	c := &cobra.Command{Use: "trade", Short: "Place and manage orders"}
	c.AddCommand(
		newTradeLimitCmd(),
		newTradeMarketCmd(),
		newTradeCancelCmd(),
		newTradeCancelAllCmd(),
		newTradeUpdateCmd(),
	)
	return c
}

// combineOrderFlags builds a single OrderFlags value from the boolean flags.
// Only one flag is supported per order per SDK convention; the function errors
// if more than one is set.
func combineOrderFlags(postOnly, ioc, reduceOnly bool) (*hibachi.OrderFlags, error) {
	set := []hibachi.OrderFlags{}
	if postOnly {
		set = append(set, hibachi.OrderFlagsPostOnly)
	}
	if ioc {
		set = append(set, hibachi.OrderFlagsIOC)
	}
	if reduceOnly {
		set = append(set, hibachi.OrderFlagsReduceOnly)
	}
	if len(set) == 0 {
		return nil, nil
	}
	if len(set) > 1 {
		return nil, fmt.Errorf("only one of --post-only, --ioc, --reduce-only may be set")
	}
	f := set[0]
	return &f, nil
}

func newTradeLimitCmd() *cobra.Command {
	var (
		postOnly, ioc, reduceOnly bool
		maxFee                    string
	)
	c := &cobra.Command{
		Use:   "limit [symbol] [side] [qty] [price]",
		Short: "Place a limit order",
		Args:  cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			symbol := args[0]
			side, err := parseSide(args[1])
			if err != nil {
				return err
			}
			qty, err := parseDecimal(args[2])
			if err != nil {
				return err
			}
			price, err := parseDecimal(args[3])
			if err != nil {
				return err
			}
			maxFees, err := parseDecimal(maxFee)
			if err != nil {
				return err
			}
			flags, err := combineOrderFlags(postOnly, ioc, reduceOnly)
			if err != nil {
				return err
			}

			a := app.From(cmd.Context())
			if err := a.EnsureClient(); err != nil {
				return err
			}

			notional, _ := qty.Mul(price).Float64()
			limits := safetyLimits(a)
			action := safety.Action{Kind: "trade.limit", Symbol: symbol, NotionalUSD: notional}
			if err := limits.Check(action); err != nil {
				return err
			}

			flagsStr := ""
			if flags != nil {
				flagsStr = string(*flags)
			}
			preview := [][2]string{
				{"kind", "limit"},
				{"symbol", symbol},
				{"side", string(side)},
				{"qty", qty.String()},
				{"price", price.String()},
				{"flags", flagsStr},
				{"max_fee", maxFees.String()},
				{"notional_usd", fmt.Sprintf("%.4f", notional)},
				{"dry_run", strconv.FormatBool(a.DryRun)},
			}
			output.PrintKV(preview)

			if a.DryRun {
				fmt.Println("[dry-run] no order placed")
				return nil
			}

			ok, err := safety.Confirm(os.Stdout, os.Stdin, "", a.Yes || !a.Cfg.Safety.RequireConfirm)
			if err != nil {
				return err
			}
			if !ok {
				fmt.Println("aborted")
				return nil
			}

			opts := []hibachi.OrderOption{}
			if flags != nil {
				opts = append(opts, hibachi.WithOrderFlags(*flags))
			}

			payload := map[string]any{
				"symbol":   symbol,
				"side":     string(side),
				"quantity": qty.String(),
				"price":    price.String(),
				"max_fee":  maxFees.String(),
				"flags":    flagsStr,
			}
			eventID, j, err := recordIntent(cmd.Context(), a, "order_placed", symbol, payload)
			if err != nil {
				return err
			}
			defer j.Close()

			res, err := a.Client.PlaceLimitOrder(cmd.Context(), symbol, side, qty, price, maxFees, opts...)
			if err != nil {
				setErrorOutcome(cmd.Context(), j, eventID, err)
				return err
			}

			setPlacedOutcome(cmd.Context(), j, eventID, res)
			return renderPlaceResult(a, res)
		},
	}
	c.Flags().BoolVar(&postOnly, "post-only", false, "post-only flag (maker only)")
	c.Flags().BoolVar(&ioc, "ioc", false, "immediate-or-cancel flag")
	c.Flags().BoolVar(&reduceOnly, "reduce-only", false, "reduce-only flag")
	c.Flags().StringVar(&maxFee, "max-fee", "0.0005", "maximum fee as a decimal rate")
	return c
}

func newTradeMarketCmd() *cobra.Command {
	var maxFee string
	c := &cobra.Command{
		Use:   "market [symbol] [side] [qty]",
		Short: "Place a market order",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			symbol := args[0]
			side, err := parseSide(args[1])
			if err != nil {
				return err
			}
			qty, err := parseDecimal(args[2])
			if err != nil {
				return err
			}
			maxFees, err := parseDecimal(maxFee)
			if err != nil {
				return err
			}

			a := app.From(cmd.Context())
			if err := a.EnsureClient(); err != nil {
				return err
			}

			// Notional for market orders is unknown without a live price; we
			// pass 0 so the cap check never blocks a market order. Symbol
			// whitelist still applies.
			limits := safetyLimits(a)
			action := safety.Action{Kind: "trade.market", Symbol: symbol, NotionalUSD: 0}
			if err := limits.Check(action); err != nil {
				return err
			}

			preview := [][2]string{
				{"kind", "market"},
				{"symbol", symbol},
				{"side", string(side)},
				{"qty", qty.String()},
				{"max_fee", maxFees.String()},
				{"dry_run", strconv.FormatBool(a.DryRun)},
			}
			output.PrintKV(preview)

			if a.DryRun {
				fmt.Println("[dry-run] no order placed")
				return nil
			}

			ok, err := safety.Confirm(os.Stdout, os.Stdin, "", a.Yes || !a.Cfg.Safety.RequireConfirm)
			if err != nil {
				return err
			}
			if !ok {
				fmt.Println("aborted")
				return nil
			}

			payload := map[string]any{
				"symbol":   symbol,
				"side":     string(side),
				"quantity": qty.String(),
				"max_fee":  maxFees.String(),
			}
			eventID, j, err := recordIntent(cmd.Context(), a, "order_placed", symbol, payload)
			if err != nil {
				return err
			}
			defer j.Close()

			res, err := a.Client.PlaceMarketOrder(cmd.Context(), symbol, side, qty, maxFees)
			if err != nil {
				setErrorOutcome(cmd.Context(), j, eventID, err)
				return err
			}
			setPlacedOutcome(cmd.Context(), j, eventID, res)
			return renderPlaceResult(a, res)
		},
	}
	c.Flags().StringVar(&maxFee, "max-fee", "0.0005", "maximum fee as a decimal rate")
	return c
}

func newTradeCancelCmd() *cobra.Command {
	var nonce int64
	c := &cobra.Command{
		Use:   "cancel [id]",
		Short: "Cancel one order",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.EnsureClient(); err != nil {
				return err
			}

			var cancel hibachi.CancelOrder
			var targetLabel string
			switch {
			case len(args) == 1:
				id, err := strconv.ParseInt(args[0], 10, 64)
				if err != nil {
					return fmt.Errorf("invalid order id %q: %w", args[0], err)
				}
				cancel.OrderID = &id
				targetLabel = fmt.Sprintf("order_id=%d", id)
			case nonce != 0:
				n := nonce
				cancel.Nonce = &n
				targetLabel = fmt.Sprintf("nonce=%d", n)
			default:
				return fmt.Errorf("provide an order id argument or --nonce")
			}

			limits := safetyLimits(a)
			if err := limits.Check(safety.Action{Kind: "trade.cancel"}); err != nil {
				return err
			}

			preview := [][2]string{
				{"kind", "cancel"},
				{"target", targetLabel},
				{"dry_run", strconv.FormatBool(a.DryRun)},
			}
			output.PrintKV(preview)

			if a.DryRun {
				fmt.Println("[dry-run] no order cancelled")
				return nil
			}

			ok, err := safety.Confirm(os.Stdout, os.Stdin, "", a.Yes || !a.Cfg.Safety.RequireConfirm)
			if err != nil {
				return err
			}
			if !ok {
				fmt.Println("aborted")
				return nil
			}

			payload := map[string]any{"target": targetLabel}
			eventID, j, err := recordIntent(cmd.Context(), a, "order_cancelled", "", payload)
			if err != nil {
				return err
			}
			defer j.Close()

			if err := a.Client.CancelOrder(cmd.Context(), cancel); err != nil {
				setErrorOutcome(cmd.Context(), j, eventID, err)
				return err
			}
			outcome, _ := json.Marshal(map[string]any{"status": "cancelled", "target": targetLabel})
			_ = j.SetOutcome(cmd.Context(), eventID, outcome)
			fmt.Printf("cancelled %s\n", targetLabel)
			return nil
		},
	}
	c.Flags().Int64Var(&nonce, "nonce", 0, "cancel by nonce instead of order id")
	return c
}

// cancelAllPending cancels every pending order and verifies the result. The
// exchange's bulk DELETE /trade/order/all occasionally returns 200 but silently
// no-ops, and per-order cancels are eventually consistent (an order can linger
// for a second or two after a successful cancel). So this issues the bulk
// cancel first, then re-checks and falls back to per-order cancels, retrying a
// few rounds with a short settle delay. Returns the count still pending (0 on
// full success).
func cancelAllPending(ctx context.Context, c *hibachi.Client) (int, error) {
	const rounds = 8
	for round := 0; round < rounds; round++ {
		pending, err := c.GetPendingOrders(ctx)
		if err != nil {
			return -1, err
		}
		if len(pending) == 0 {
			return 0, nil
		}
		if round == 0 {
			_ = c.CancelAllOrders(ctx) // bulk attempt first
		} else {
			for _, o := range pending { // per-order fallback for stragglers
				id := o.OrderID
				_ = c.CancelOrder(ctx, hibachi.CancelOrder{OrderID: &id})
			}
		}
		select {
		case <-ctx.Done():
			return len(pending), ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	pending, err := c.GetPendingOrders(ctx)
	if err != nil {
		return -1, err
	}
	return len(pending), nil
}

func newTradeCancelAllCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel-all",
		Short: "Cancel all pending orders",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := app.From(cmd.Context())
			if err := a.EnsureClient(); err != nil {
				return err
			}

			limits := safetyLimits(a)
			if err := limits.Check(safety.Action{Kind: "trade.cancel_all"}); err != nil {
				return err
			}

			preview := [][2]string{
				{"kind", "cancel-all"},
				{"dry_run", strconv.FormatBool(a.DryRun)},
			}
			output.PrintKV(preview)

			if a.DryRun {
				fmt.Println("[dry-run] no orders cancelled")
				return nil
			}

			ok, err := safety.Confirm(os.Stdout, os.Stdin, "This will cancel ALL pending orders.", a.Yes)
			if err != nil {
				return err
			}
			if !ok {
				fmt.Println("aborted")
				return nil
			}

			eventID, j, err := recordIntent(cmd.Context(), a, "orders_cancelled_all", "", map[string]any{})
			if err != nil {
				return err
			}
			defer j.Close()

			remaining, err := cancelAllPending(cmd.Context(), a.Client)
			if err != nil {
				setErrorOutcome(cmd.Context(), j, eventID, err)
				return err
			}
			if remaining > 0 {
				outcome, _ := json.Marshal(map[string]any{"status": "partial", "remaining": remaining})
				_ = j.SetOutcome(cmd.Context(), eventID, outcome)
				fmt.Printf("%d order(s) still pending after retries (exchange lag); re-run to clear\n", remaining)
				return nil
			}
			outcome, _ := json.Marshal(map[string]any{"status": "cancelled_all"})
			_ = j.SetOutcome(cmd.Context(), eventID, outcome)
			fmt.Println("cancelled all pending orders")
			return nil
		},
	}
}

func newTradeUpdateCmd() *cobra.Command {
	var (
		priceStr, qtyStr, sideStr string
		deadline                  int64
		maxFee                    string
	)
	c := &cobra.Command{
		Use:   "update [id]",
		Short: "Update an order (price or quantity)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid order id %q: %w", args[0], err)
			}

			a := app.From(cmd.Context())
			if err := a.EnsureClient(); err != nil {
				return err
			}

			existing, err := a.Client.GetOrderDetails(cmd.Context(), &id, nil)
			if err != nil {
				return fmt.Errorf("fetch order %d: %w", id, err)
			}

			upd := hibachi.UpdateOrder{
				OrderID: id,
				Symbol:  existing.Symbol,
				Side:    existing.Side,
			}

			// Quantity: start from existing total then override from flag.
			if existing.TotalQuantity != nil {
				q, err := parseDecimal(*existing.TotalQuantity)
				if err != nil {
					return fmt.Errorf("parse existing quantity: %w", err)
				}
				upd.Quantity = q
			}
			if qtyStr != "" {
				q, err := parseDecimal(qtyStr)
				if err != nil {
					return err
				}
				upd.Quantity = q
			}

			// Price: start from existing (if limit) then override from flag.
			if existing.Price != nil && *existing.Price != "" {
				p, err := parseDecimal(*existing.Price)
				if err != nil {
					return fmt.Errorf("parse existing price: %w", err)
				}
				upd.Price = &p
			}
			if priceStr != "" {
				p, err := parseDecimal(priceStr)
				if err != nil {
					return err
				}
				upd.Price = &p
			}

			if sideStr != "" {
				s, err := parseSide(sideStr)
				if err != nil {
					return err
				}
				upd.Side = s
			}

			// Max fee: required field on UpdateOrder. Default matches other commands.
			feeStr := maxFee
			if feeStr == "" {
				feeStr = "0.0005"
			}
			maxFees, err := parseDecimal(feeStr)
			if err != nil {
				return err
			}
			upd.MaxFeesPercent = maxFees

			// --deadline is seconds from now. The API expects an absolute
			// microsecond timestamp (the legacy float-seconds format is
			// rejected), so convert before sending.
			if deadline != 0 {
				d := time.Now().Add(time.Duration(deadline) * time.Second).UnixMicro()
				upd.CreationDeadline = &d
			}

			// Validate required fields post-merge.
			if upd.Quantity.IsZero() {
				return fmt.Errorf("quantity is zero; pass --qty")
			}

			notional := 0.0
			if upd.Price != nil {
				notional, _ = upd.Quantity.Mul(*upd.Price).Float64()
			}
			limits := safetyLimits(a)
			action := safety.Action{Kind: "trade.update", Symbol: upd.Symbol, NotionalUSD: notional}
			if err := limits.Check(action); err != nil {
				return err
			}

			priceDisplay := ""
			if upd.Price != nil {
				priceDisplay = upd.Price.String()
			}
			preview := [][2]string{
				{"kind", "update"},
				{"order_id", strconv.FormatInt(id, 10)},
				{"symbol", upd.Symbol},
				{"side", string(upd.Side)},
				{"qty", upd.Quantity.String()},
				{"price", priceDisplay},
				{"max_fee", maxFees.String()},
				{"notional_usd", fmt.Sprintf("%.4f", notional)},
				{"dry_run", strconv.FormatBool(a.DryRun)},
			}
			output.PrintKV(preview)

			if a.DryRun {
				fmt.Println("[dry-run] no order updated")
				return nil
			}

			ok, err := safety.Confirm(os.Stdout, os.Stdin, "", a.Yes || !a.Cfg.Safety.RequireConfirm)
			if err != nil {
				return err
			}
			if !ok {
				fmt.Println("aborted")
				return nil
			}

			payload := map[string]any{
				"order_id": id,
				"symbol":   upd.Symbol,
				"side":     string(upd.Side),
				"quantity": upd.Quantity.String(),
				"price":    priceDisplay,
				"max_fee":  maxFees.String(),
			}
			eventID, j, err := recordIntent(cmd.Context(), a, "order_updated", upd.Symbol, payload)
			if err != nil {
				return err
			}
			defer j.Close()

			res, err := a.Client.UpdateOrder(cmd.Context(), upd)
			if err != nil {
				setErrorOutcome(cmd.Context(), j, eventID, err)
				return err
			}
			setPlacedOutcome(cmd.Context(), j, eventID, res)
			return renderPlaceResult(a, res)
		},
	}
	c.Flags().StringVar(&priceStr, "price", "", "new price")
	c.Flags().StringVar(&qtyStr, "qty", "", "new quantity")
	c.Flags().StringVar(&sideStr, "side", "", "new side (BUY/SELL/BID/ASK)")
	c.Flags().Int64Var(&deadline, "deadline", 0, "reject the order if not processed within N seconds (0 = no deadline)")
	c.Flags().StringVar(&maxFee, "max-fee", "0.0005", "maximum fee as a decimal rate")
	return c
}

// recordIntent opens the journal and records an event before the SDK call.
// The returned Journal must be closed by the caller.
func recordIntent(ctx context.Context, a *app.App, kind, symbol string, payload map[string]any) (int64, *journal.Journal, error) {
	j, err := journal.Open(a.Cfg.Journal.Path)
	if err != nil {
		return 0, nil, fmt.Errorf("open journal: %w", err)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		j.Close()
		return 0, nil, fmt.Errorf("marshal payload: %w", err)
	}
	ev := journal.Event{
		Kind:    kind,
		Symbol:  symbol,
		Agent:   "cli",
		Payload: raw,
	}
	id, err := j.Record(ctx, ev)
	if err != nil {
		j.Close()
		return 0, nil, err
	}
	return id, j, nil
}

func setPlacedOutcome(ctx context.Context, j *journal.Journal, id int64, res *hibachi.PlaceOrderResult) {
	if res == nil {
		return
	}
	outcome, _ := json.Marshal(map[string]any{
		"status":   "placed",
		"order_id": res.OrderID,
		"nonce":    res.Nonce,
	})
	_ = j.SetOutcome(ctx, id, outcome)
}

func setErrorOutcome(ctx context.Context, j *journal.Journal, id int64, err error) {
	outcome, _ := json.Marshal(map[string]any{
		"status":  "error",
		"message": err.Error(),
	})
	_ = j.SetOutcome(ctx, id, outcome)
}

func renderPlaceResult(a *app.App, res *hibachi.PlaceOrderResult) error {
	if a.JSON {
		return output.PrintJSON(res)
	}
	output.PrintKV([][2]string{
		{"order_id", res.OrderID},
		{"nonce", res.Nonce},
	})
	return nil
}
