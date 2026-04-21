package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/doedja/hibachi-cli/internal/aiagent"
	"github.com/doedja/hibachi-cli/internal/aiagent/backend"
	"github.com/doedja/hibachi-cli/internal/app"
	"github.com/doedja/hibachi-cli/internal/journal"
	"github.com/doedja/hibachi-cli/internal/memory"
	"github.com/doedja/hibachi-cli/internal/strategies"

	// Import each strategy subpackage for its init-time registration.
	_ "github.com/doedja/hibachi-cli/internal/strategies/advisor"
	_ "github.com/doedja/hibachi-cli/internal/strategies/dca"
	_ "github.com/doedja/hibachi-cli/internal/strategies/grid"
	_ "github.com/doedja/hibachi-cli/internal/strategies/tpsl"
)

func newAgentCmd() *cobra.Command {
	c := &cobra.Command{Use: "agent", Short: "Run bundled strategy agents"}
	c.AddCommand(
		newAgentListCmd(),
		newAgentRunCmd(),
		newAgentStopCmd(),
	)
	return c
}

func newAgentListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available strategies",
		RunE: func(cmd *cobra.Command, _ []string) error {
			for _, name := range strategies.Available() {
				s, err := strategies.Build(name)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%-10s %s\n", s.Name(), s.Description())
			}
			return nil
		},
	}
}

func newAgentRunCmd() *cobra.Command {
	c := &cobra.Command{
		Use:                "run [kind] [flags...]",
		Short:              "Run a strategy (dca | grid | tpsl | advisor)",
		Args:               cobra.MinimumNArgs(1),
		DisableFlagParsing: true,
		RunE:               runAgentRun,
	}
	return c
}

func runAgentRun(cmd *cobra.Command, args []string) error {
	// DisableFlagParsing is on so the strategy owns its flags. We still need
	// to let `--help` on the run subcommand itself work, so honour it here
	// before hand-off.
	if len(args) >= 1 && (args[0] == "-h" || args[0] == "--help") {
		return cmd.Help()
	}
	kind := args[0]
	rest := args[1:]
	if len(rest) == 1 && (rest[0] == "-h" || rest[0] == "--help") {
		s, err := strategies.Build(kind)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n\nUsage:\n  hibachi agent run %s [flags]\n\n", s.Name(), s.Description(), s.Name())
		// ParseFlags with --help triggers the flag package's usage output.
		_ = s.ParseFlags([]string{"-h"})
		return nil
	}

	s, err := strategies.Build(kind)
	if err != nil {
		return err
	}
	if err := s.ParseFlags(rest); err != nil {
		return err
	}

	a := app.From(cmd.Context())
	if err := a.EnsureClient(); err != nil {
		return err
	}
	// Signer only needed if we actually place orders (not dry-run). Best-effort.
	if !a.DryRun {
		if err := a.EnsureSigner(); err != nil {
			fmt.Fprintf(os.Stderr, "warn: signer not available (%v); strategy will fail if it tries to place an order\n", err)
		}
	}

	j, err := journal.Open(a.Cfg.Journal.Path)
	if err != nil {
		return fmt.Errorf("open journal: %w", err)
	}
	defer j.Close()

	var mem *memory.Store
	if a.Cfg.Memory.Dir != "" {
		if m, err := memory.Open(a.Cfg.Memory.Dir); err == nil {
			mem = m
		}
	}

	var planner aiagent.Planner
	if s.Name() == "advisor" {
		p, err := backend.New(a.Cfg)
		if err != nil {
			return fmt.Errorf("advisor backend: %w", err)
		}
		defer p.Close()
		planner = p
	}

	ctx, cancel := signalContext(cmd.Context())
	defer cancel()

	deps := strategies.AgentDeps{
		Client:  a.Client,
		Signer:  a.Signer,
		Cfg:     a.Cfg,
		Journal: j,
		Memory:  mem,
		Planner: planner,
		Safety:  safetyLimits(a),
		DryRun:  a.DryRun,
		Logger:  func(line string) { fmt.Println(line) },
	}
	return s.Run(ctx, deps)
}

func newAgentStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop [id]",
		Short: "Stop a running agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "v0.1: stop with Ctrl+C on the running terminal")
			return nil
		},
	}
}

// signalContext returns a ctx that cancels on SIGINT/SIGTERM.
func signalContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-ch:
			cancel()
		case <-ctx.Done():
		}
		signal.Stop(ch)
	}()
	return ctx, cancel
}
