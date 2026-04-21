package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/doedja/hibachi-cli/internal/app"
	"github.com/doedja/hibachi-cli/internal/config"
)

var (
	flagConfig string
	flagDryRun bool
	flagYes    bool
	flagJSON   bool
	flagLive   bool
	flagAI     string
	flagModel  string
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "hibachi",
		Short:         "Hibachi perpetual futures CLI",
		Long:          "Unofficial command-line client for the Hibachi perpetual futures exchange. Not affiliated with Hibachi.",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(resolveConfigPath())
			if err != nil {
				return err
			}
			if flagAI != "" {
				cfg.AI.Backend = flagAI
			}
			if flagModel != "" {
				switch cfg.AI.Backend {
				case "openrouter":
					cfg.AI.OpenRouter.Model = flagModel
				case "claude-code":
					cfg.AI.ClaudeCode.Model = flagModel
				}
			}
			a := app.Build(cfg)
			a.DryRun = flagDryRun || (cfg.Safety.DryRunDefault && !flagLive)
			a.Yes = flagYes
			a.JSON = flagJSON
			cmd.SetContext(app.Into(cmd.Context(), a))
			return nil
		},
	}

	root.PersistentFlags().StringVar(&flagConfig, "config", "", "config file (default $HIBACHI_CONFIG or ~/.config/hibachi/config.toml)")
	root.PersistentFlags().BoolVar(&flagDryRun, "dry-run", false, "preview actions without submitting")
	root.PersistentFlags().BoolVar(&flagLive, "live", false, "override safety.dry_run_default = true")
	root.PersistentFlags().BoolVarP(&flagYes, "yes", "y", false, "skip confirmation prompts")
	root.PersistentFlags().BoolVar(&flagJSON, "json", false, "machine-readable output")
	root.PersistentFlags().StringVar(&flagAI, "ai", "", "override ai backend (claude-code | openrouter)")
	root.PersistentFlags().StringVar(&flagModel, "model", "", "override ai model")

	root.AddCommand(
		newVersionCmd(),
		newAuthCmd(),
		newMarketCmd(),
		newAccountCmd(),
		newTradeCmd(),
		newCapitalCmd(),
		newStreamCmd(),
		newDashCmd(),
		newAICmd(),
		newAgentCmd(),
		newMemoryCmd(),
	)

	return root
}

// Execute runs the root command with natural-language fallback.
// If the first positional arg is not a registered subcommand, the args are
// rerouted to `ai` so the AI planner receives them as a prompt.
func Execute() error {
	root := newRootCmd()
	args := os.Args[1:]

	if rerouted, ok := maybeRerouteToAI(root, args); ok {
		root.SetArgs(rerouted)
	}

	return root.Execute()
}

// maybeRerouteToAI returns (newArgs, true) if the arguments should be treated
// as a natural-language prompt for the AI planner. The rule: if any positional
// token matches a registered top-level subcommand, let cobra handle it;
// otherwise prepend `ai` so the rest is parsed as the AI prompt (with its own
// flags like --fresh).
func maybeRerouteToAI(root *cobra.Command, args []string) ([]string, bool) {
	if len(args) == 0 {
		return nil, false
	}

	// Leave cobra alone for help and completion.
	for _, a := range args {
		if a == "help" || a == "completion" || a == "-h" || a == "--help" {
			return nil, false
		}
	}

	// Build the set of known subcommand names so we don't reroute on real
	// commands.
	known := make(map[string]bool, 16)
	for _, c := range root.Commands() {
		known[c.Name()] = true
		for _, al := range c.Aliases {
			known[al] = true
		}
	}

	// If any token matches a known subcommand, cobra can handle it.
	for _, a := range args {
		if known[a] {
			return nil, false
		}
	}

	// Prepend `ai` so subcommand-level flags (--fresh) attach to the ai
	// command, and global persistent flags still work since they propagate.
	return append([]string{"ai"}, args...), true
}

func resolveConfigPath() string {
	if flagConfig != "" {
		return flagConfig
	}
	return config.DefaultPath()
}
