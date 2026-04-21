package aiagent

import "encoding/json"

// Action kinds. Keep the list small: the executor must handle every one.
const (
	ActionTradeLimit      = "trade.limit"
	ActionTradeMarket     = "trade.market"
	ActionTradeCancel     = "trade.cancel"
	ActionTradeCancelAll  = "trade.cancel_all"
	ActionTradeUpdate     = "trade.update"
	ActionTradeTPSL       = "trade.tpsl"
	ActionCapitalTransfer = "capital.transfer"
	ActionGetContext      = "get_context"
	ActionDone            = "done"
)

type Plan struct {
	Actions       []Action      `json:"actions"`
	MemoryWrites  []MemoryWrite `json:"memory_writes,omitempty"`
	MemoryDeletes []string      `json:"memory_deletes,omitempty"`
	Reasoning     string        `json:"reasoning,omitempty"`
	Ask           string        `json:"ask,omitempty"`
}

type Action struct {
	Kind    string          `json:"kind"`
	Symbol  string          `json:"symbol,omitempty"`
	Side    string          `json:"side,omitempty"`
	Qty     string          `json:"qty,omitempty"`
	Price   string          `json:"price,omitempty"`
	OrderID *int64          `json:"order_id,omitempty"`
	TP      string          `json:"tp,omitempty"`
	SL      string          `json:"sl,omitempty"`
	Extra   json.RawMessage `json:"extra,omitempty"`
	Reason  string          `json:"reason,omitempty"`
}

type MemoryWrite struct {
	File    string `json:"file"`
	Content string `json:"content"`
}

// SchemaJSON returns a compact description of the Plan envelope that
// gets inlined into the system prompt. Kept as a single string so it
// ships verbatim to the model.
func SchemaJSON() string {
	return `{
  "actions": [
    {
      "kind": "trade.limit | trade.market | trade.cancel | trade.cancel_all | trade.update | trade.tpsl | capital.transfer | get_context | done",
      "symbol": "e.g. BTC/USDT-P",
      "side": "BUY | SELL",
      "qty": "decimal string, coin units (not USD notional)",
      "price": "decimal string, required for trade.limit and trade.update",
      "order_id": 123456789,
      "tp": "decimal string, for trade.tpsl",
      "sl": "decimal string, for trade.tpsl",
      "extra": { "any": "structured data, e.g. transfer target" },
      "reason": "short human-readable explanation of this single action"
    }
  ],
  "memory_writes": [
    { "file": "notes.md", "content": "full replacement contents" }
  ],
  "memory_deletes": ["stale.md"],
  "reasoning": "one-paragraph explanation for the whole plan; keep terse",
  "ask": "when set, treat as a clarification question instead of a plan; leave actions empty"
}`
}
