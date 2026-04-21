package aiagent

import (
	"context"
	"fmt"

	hibachi "github.com/doedja/hibachi-go"
	"github.com/shopspring/decimal"
)

type ActionResult struct {
	Action  Action
	OK      bool
	Message string
	OrderID int64
	Err     error
}

type ExecutorDeps struct {
	Client      *hibachi.Client
	Signer      hibachi.Signer
	SafetyCheck func(kind, symbol string, notionalUSD float64) error
	OnAction    func(a Action, r ActionResult)
}

// Execute runs each action sequentially. A failing action does not abort
// the rest; callers inspect per-action OK/Err.
func Execute(ctx context.Context, deps ExecutorDeps, plan Plan) []ActionResult {
	results := make([]ActionResult, 0, len(plan.Actions))
	for _, a := range plan.Actions {
		r := runAction(ctx, deps, a)
		results = append(results, r)
		if deps.OnAction != nil {
			deps.OnAction(a, r)
		}
	}
	return results
}

func runAction(ctx context.Context, deps ExecutorDeps, a Action) ActionResult {
	switch a.Kind {
	case ActionTradeLimit:
		return execLimit(ctx, deps, a)
	case ActionTradeMarket:
		return execMarket(ctx, deps, a)
	case ActionTradeCancel:
		return execCancel(ctx, deps, a)
	case ActionTradeCancelAll:
		return execCancelAll(ctx, deps, a)
	case ActionTradeUpdate:
		return execUpdate(ctx, deps, a)
	case ActionTradeTPSL:
		return execTPSL(ctx, deps, a)
	case ActionCapitalTransfer:
		return fail(a, "capital.transfer not implemented", nil)
	case ActionGetContext, ActionDone:
		return ok(a, "no-op", 0)
	default:
		return fail(a, fmt.Sprintf("unknown action kind %q", a.Kind), nil)
	}
}

func execLimit(ctx context.Context, deps ExecutorDeps, a Action) ActionResult {
	qty, price, err := parseQtyPrice(a, true)
	if err != nil {
		return fail(a, err.Error(), err)
	}
	if err := safety(deps, a, qty.Mul(price)); err != nil {
		return fail(a, err.Error(), err)
	}
	side, err := parseSide(a.Side)
	if err != nil {
		return fail(a, err.Error(), err)
	}
	res, err := deps.Client.PlaceLimitOrder(ctx, a.Symbol, side, qty, price, maxFees())
	if err != nil {
		return fail(a, "place limit failed", err)
	}
	return ok(a, "limit placed", parseOrderID(res))
}

func execMarket(ctx context.Context, deps ExecutorDeps, a Action) ActionResult {
	qty, err := decimalOrErr("qty", a.Qty)
	if err != nil {
		return fail(a, err.Error(), err)
	}
	if err := safety(deps, a, qty); err != nil {
		return fail(a, err.Error(), err)
	}
	side, err := parseSide(a.Side)
	if err != nil {
		return fail(a, err.Error(), err)
	}
	res, err := deps.Client.PlaceMarketOrder(ctx, a.Symbol, side, qty, maxFees())
	if err != nil {
		return fail(a, "place market failed", err)
	}
	return ok(a, "market placed", parseOrderID(res))
}

func execCancel(ctx context.Context, deps ExecutorDeps, a Action) ActionResult {
	if a.OrderID == nil {
		return fail(a, "order_id required for trade.cancel", nil)
	}
	if err := deps.Client.CancelOrder(ctx, hibachi.CancelOrder{OrderID: a.OrderID}); err != nil {
		return fail(a, "cancel failed", err)
	}
	return ok(a, "cancelled", *a.OrderID)
}

func execCancelAll(ctx context.Context, deps ExecutorDeps, a Action) ActionResult {
	if err := deps.Client.CancelAllOrders(ctx); err != nil {
		return fail(a, "cancel all failed", err)
	}
	return ok(a, "all cancelled", 0)
}

func execUpdate(ctx context.Context, deps ExecutorDeps, a Action) ActionResult {
	if a.OrderID == nil {
		return fail(a, "order_id required for trade.update", nil)
	}
	qty, err := decimalOrErr("qty", a.Qty)
	if err != nil {
		return fail(a, err.Error(), err)
	}
	side, err := parseSide(a.Side)
	if err != nil {
		return fail(a, err.Error(), err)
	}
	var pricePtr *decimal.Decimal
	if a.Price != "" {
		p, err := decimalOrErr("price", a.Price)
		if err != nil {
			return fail(a, err.Error(), err)
		}
		pricePtr = &p
	}
	upd := hibachi.UpdateOrder{
		OrderID:        *a.OrderID,
		Symbol:         a.Symbol,
		Side:           side,
		Quantity:       qty,
		MaxFeesPercent: maxFees(),
		Price:          pricePtr,
	}
	res, err := deps.Client.UpdateOrder(ctx, upd)
	if err != nil {
		return fail(a, "update failed", err)
	}
	return ok(a, "updated", parseOrderID(res))
}

func execTPSL(ctx context.Context, deps ExecutorDeps, a Action) ActionResult {
	qty, price, err := parseQtyPrice(a, true)
	if err != nil {
		return fail(a, err.Error(), err)
	}
	side, err := parseSide(a.Side)
	if err != nil {
		return fail(a, err.Error(), err)
	}
	if err := safety(deps, a, qty.Mul(price)); err != nil {
		return fail(a, err.Error(), err)
	}
	cfg := &hibachi.TPSLConfig{}
	if a.TP != "" {
		tp, err := decimalOrErr("tp", a.TP)
		if err != nil {
			return fail(a, err.Error(), err)
		}
		cfg.AddTakeProfit(tp, nil)
	}
	if a.SL != "" {
		sl, err := decimalOrErr("sl", a.SL)
		if err != nil {
			return fail(a, err.Error(), err)
		}
		cfg.AddStopLoss(sl, nil)
	}
	if len(cfg.Legs) == 0 {
		return fail(a, "trade.tpsl needs tp or sl", nil)
	}
	res, err := deps.Client.PlaceLimitOrder(ctx, a.Symbol, side, qty, price, maxFees(), hibachi.WithTPSL(*cfg))
	if err != nil {
		return fail(a, "place tpsl failed", err)
	}
	return ok(a, "tpsl placed", parseOrderID(res))
}

func parseQtyPrice(a Action, requirePrice bool) (decimal.Decimal, decimal.Decimal, error) {
	qty, err := decimalOrErr("qty", a.Qty)
	if err != nil {
		return decimal.Zero, decimal.Zero, err
	}
	if !requirePrice && a.Price == "" {
		return qty, decimal.Zero, nil
	}
	price, err := decimalOrErr("price", a.Price)
	if err != nil {
		return decimal.Zero, decimal.Zero, err
	}
	return qty, price, nil
}

func decimalOrErr(field, v string) (decimal.Decimal, error) {
	if v == "" {
		return decimal.Zero, fmt.Errorf("missing %s", field)
	}
	d, err := decimal.NewFromString(v)
	if err != nil {
		return decimal.Zero, fmt.Errorf("invalid %s %q: %w", field, v, err)
	}
	return d, nil
}

func parseSide(s string) (hibachi.Side, error) {
	switch s {
	case "BUY", "BID":
		return hibachi.SideBid, nil
	case "SELL", "ASK":
		return hibachi.SideAsk, nil
	default:
		return "", fmt.Errorf("invalid side %q", s)
	}
}

// maxFees uses a conservative default; callers can override later by
// wiring it through the executor once the config layer exposes it.
func maxFees() decimal.Decimal {
	return decimal.NewFromFloat(0.0005)
}

func safety(deps ExecutorDeps, a Action, notional decimal.Decimal) error {
	if deps.SafetyCheck == nil {
		return nil
	}
	f, _ := notional.Float64()
	return deps.SafetyCheck(a.Kind, a.Symbol, f)
}

func parseOrderID(res *hibachi.PlaceOrderResult) int64 {
	if res == nil {
		return 0
	}
	var id int64
	fmt.Sscanf(res.OrderID, "%d", &id)
	return id
}

func ok(a Action, msg string, id int64) ActionResult {
	return ActionResult{Action: a, OK: true, Message: msg, OrderID: id}
}

func fail(a Action, msg string, err error) ActionResult {
	return ActionResult{Action: a, OK: false, Message: msg, Err: err}
}
