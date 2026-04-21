package dash

import (
	"encoding/json"
	"time"

	hibachi "github.com/doedja/hibachi-go"

	"github.com/doedja/hibachi-cli/internal/aiagent"
)

// TickMsg fires every second for clock + age recomputation.
type TickMsg struct{ T time.Time }

// OrderbookMsg carries a fresh orderbook snapshot for the focused symbol.
type OrderbookMsg struct {
	Symbol string
	Book   *hibachi.OrderBook
}

// TradeMsg carries a single public trade.
type TradeMsg struct {
	Symbol    string
	Price     string
	Quantity  string
	Timestamp int64
	Taker     string
}

// PriceTickMsg carries a price update for a watchlist symbol.
type PriceTickMsg struct {
	Symbol string
	Mark   string
	Ask    string
	Bid    string
}

// AccountSnapshotMsg replaces account state (initial + reconnect).
type AccountSnapshotMsg struct {
	Snapshot *hibachi.AccountSnapshot
}

// AccountEventMsg is a generic account-stream event.
type AccountEventMsg struct {
	Topic string
	Data  json.RawMessage
}

// PendingOrdersMsg replaces the open orders list.
type PendingOrdersMsg struct {
	Orders []hibachi.Order
}

// AccountInfoMsg updates the top strip balance + notional fields.
type AccountInfoMsg struct {
	Info *hibachi.AccountInfo
}

// MarketStatusMsg updates the live/reconnecting pill.
type MarketStatusMsg struct {
	Status     ConnState
	Reason     string
	UpdatedAt  time.Time
}

// AccountStatusMsg updates the account-stream status.
type AccountStatusMsg struct {
	Status    ConnState
	Reason    string
	UpdatedAt time.Time
}

// AdvisorTickMsg is the latest advisor_tick payload from the journal tail.
type AdvisorTickMsg struct {
	At      time.Time
	Session string
	Body    string
	Reason  string
	Raw     json.RawMessage
}

// PlanMsg is the result of running the AI planner on user input.
type PlanMsg struct {
	Plan   aiagent.Plan
	Resp   *aiagent.Response
	Prompt string
	Err    error
}

// ExecuteResultMsg is emitted after Execute finishes applying a confirmed plan.
type ExecuteResultMsg struct {
	Results []aiagent.ActionResult
	Err     error
}

// BannerMsg flashes a short notice in the footer (used by v0.2 stubs).
type BannerMsg struct {
	Text string
	TTL  time.Duration
}

// ErrorMsg surfaces a background error into the UI.
type ErrorMsg struct{ Err error }

// RefreshMsg requests a manual REST refresh pass.
type RefreshMsg struct{}

// ClearBannerMsg clears a banner after its TTL.
type ClearBannerMsg struct{}
