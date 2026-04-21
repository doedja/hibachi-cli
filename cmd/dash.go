package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/doedja/hibachi-cli/internal/aiagent"
	"github.com/doedja/hibachi-cli/internal/aiagent/backend"
	"github.com/doedja/hibachi-cli/internal/app"
	"github.com/doedja/hibachi-cli/internal/dash"
	"github.com/doedja/hibachi-cli/internal/journal"
	"github.com/doedja/hibachi-cli/internal/memory"
)

func newDashCmd() *cobra.Command {
	var symbol string
	c := &cobra.Command{
		Use:   "dash",
		Short: "Live trading dashboard (TUI)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := app.From(cmd.Context())
			if err := a.EnsureClient(); err != nil {
				return err
			}

			initialSym := symbol
			if initialSym == "" {
				info, err := a.Client.GetExchangeInfo(cmd.Context())
				if err != nil {
					return fmt.Errorf("exchange info: %w", err)
				}
				if info != nil && len(info.FutureContracts) > 0 {
					initialSym = info.FutureContracts[0].Symbol
				}
			}

			var planner aiagent.Planner
			if p, err := backend.New(a.Cfg); err == nil {
				planner = p
				defer planner.Close()
			}

			var j *journal.Journal
			if a.Cfg.Journal.Path != "" {
				if opened, err := journal.Open(a.Cfg.Journal.Path); err == nil {
					j = opened
					defer j.Close()
				}
			}

			var mem *memory.Store
			if a.Cfg.Memory.Dir != "" {
				if s, err := memory.Open(a.Cfg.Memory.Dir); err == nil {
					mem = s
				}
			}

			deps := dash.Deps{
				App:        a,
				Client:     a.Client,
				Signer:     a.Signer,
				Cfg:        a.Cfg,
				Planner:    planner,
				Journal:    j,
				Memory:     mem,
				InitialSym: initialSym,
			}
			return dash.Run(cmd.Context(), deps)
		},
	}
	c.Flags().StringVar(&symbol, "symbol", "", "initial focused symbol (default: first contract in exchange info)")
	return c
}
