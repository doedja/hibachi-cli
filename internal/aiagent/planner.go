package aiagent

import (
	"context"
	"encoding/json"
	"fmt"
)

// Planner is the abstract interface over any AI backend that turns a
// user prompt + context payload into a structured Plan.
type Planner interface {
	Plan(ctx context.Context, req Request) (*Response, error)
	Backend() string
	Model() string
	Close() error
}

type Request struct {
	SessionID    string
	SystemPrompt string
	UserPayload  json.RawMessage
	Fresh        bool
}

type Response struct {
	SessionID string
	Content   json.RawMessage
	NumTurns  int
	TokensIn  int
	TokensOut int
	CostUSD   float64
	RawText   string
}

// PlanError carries a coarse Kind so callers can branch on the category
// without matching error strings.
type PlanError struct {
	Kind    string
	Message string
	Err     error
}

func (e *PlanError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Kind, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

func (e *PlanError) Unwrap() error { return e.Err }

const (
	ErrKindUnauthorized = "unauthorized"
	ErrKindUnavailable  = "unavailable"
	ErrKindRateLimited  = "rate_limited"
	ErrKindTimeout      = "timeout"
	ErrKindBadResponse  = "bad_response"
)
