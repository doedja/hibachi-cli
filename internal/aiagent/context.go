package aiagent

import (
	"encoding/json"
	"fmt"
)

type ContextInput struct {
	Trigger    string
	UserPrompt string
	Memory     string
	Account    json.RawMessage
	Market     json.RawMessage
	Contracts  json.RawMessage
}

// BuildPayload produces the single compact JSON object that the CLI sends
// as the user-turn body. The model reads these fields; structure stable.
func BuildPayload(in ContextInput) (json.RawMessage, error) {
	out := map[string]any{
		"trigger":     in.Trigger,
		"user_prompt": in.UserPrompt,
		"memory":      in.Memory,
	}
	if len(in.Account) > 0 {
		out["account"] = json.RawMessage(in.Account)
	}
	if len(in.Market) > 0 {
		out["market"] = json.RawMessage(in.Market)
	}
	if len(in.Contracts) > 0 {
		out["contracts"] = json.RawMessage(in.Contracts)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("marshal context payload: %w", err)
	}
	return b, nil
}
