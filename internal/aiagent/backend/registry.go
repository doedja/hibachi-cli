package backend

import (
	"fmt"

	"github.com/doedja/hibachi-cli/internal/aiagent"
	"github.com/doedja/hibachi-cli/internal/aiagent/backend/claudecode"
	"github.com/doedja/hibachi-cli/internal/aiagent/backend/openrouter"
	"github.com/doedja/hibachi-cli/internal/config"
)

// New builds a Planner from config.
func New(cfg *config.Config) (aiagent.Planner, error) {
	switch cfg.AI.Backend {
	case "", "claude-code":
		return claudecode.New(cfg.AI.ClaudeCode)
	case "openrouter":
		return openrouter.New(cfg.AI.OpenRouter)
	default:
		return nil, fmt.Errorf("unknown ai.backend %q (valid: claude-code, openrouter)", cfg.AI.Backend)
	}
}
