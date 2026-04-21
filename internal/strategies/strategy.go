// Package strategies holds the four bundled strategy runners: dca, grid,
// tpsl, and advisor. Each strategy implements the Strategy interface and
// registers a factory in its own init(). The command layer imports the
// subpackages for their registration side effects.
package strategies

import (
	"context"
	"fmt"
	"sort"

	hibachi "github.com/doedja/hibachi-go"

	"github.com/doedja/hibachi-cli/internal/aiagent"
	"github.com/doedja/hibachi-cli/internal/config"
	"github.com/doedja/hibachi-cli/internal/journal"
	"github.com/doedja/hibachi-cli/internal/memory"
	"github.com/doedja/hibachi-cli/internal/safety"
)

// Strategy is a long-lived agent that manages orders for a single symbol or
// set of symbols. Run must respect ctx cancellation.
type Strategy interface {
	Name() string
	Description() string
	ParseFlags(args []string) error
	Run(ctx context.Context, deps AgentDeps) error
}

// AgentDeps bundles the shared state each strategy needs. Journal is always
// set; Memory and Planner may be nil. DryRun mirrors the global flag.
type AgentDeps struct {
	Client  *hibachi.Client
	Signer  hibachi.Signer
	Cfg     *config.Config
	Journal *journal.Journal
	Memory  *memory.Store
	Planner aiagent.Planner
	Safety  safety.Limits
	DryRun  bool
	Logger  func(string)
}

// Factory produces a new Strategy instance. Each agent run gets a fresh one.
type Factory func() Strategy

var registry = map[string]Factory{}

// Register adds a strategy factory under the given name. Called from the
// init() of each strategy subpackage.
func Register(name string, f Factory) {
	if name == "" || f == nil {
		return
	}
	registry[name] = f
}

// Available returns the sorted list of registered strategy names.
func Available() []string {
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Build constructs a strategy by name, or returns an error if not found.
func Build(name string) (Strategy, error) {
	f, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown strategy %q (available: %v)", name, Available())
	}
	return f(), nil
}
