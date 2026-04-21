package cmd

import (
	"github.com/spf13/cobra"

	"github.com/doedja/hibachi-cli/internal/app"
	"github.com/doedja/hibachi-cli/internal/output"
)

func newCapitalCmd() *cobra.Command {
	c := &cobra.Command{Use: "capital", Short: "Deposits, withdrawals, transfers"}
	c.AddCommand(
		newCapitalBalanceCmd(),
		newCapitalHistoryCmd(),
		newCapitalDepositInfoCmd(),
		// Write-path commands are gated behind a future safety wave.
		&cobra.Command{Use: "withdraw [coin] [amount] [address]", Short: "Withdraw funds", Args: cobra.ExactArgs(3), RunE: notImplemented},
		&cobra.Command{Use: "transfer [to-account] [coin] [amount]", Short: "Transfer between accounts", Args: cobra.ExactArgs(3), RunE: notImplemented},
	)
	return c
}

func newCapitalBalanceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "balance",
		Short: "Capital balance",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := app.From(cmd.Context())
			if err := a.EnsureClient(); err != nil {
				return err
			}
			total, avail, locked, extra, err := fetchBalance(cmd.Context(), a)
			if err != nil {
				return err
			}
			if a.JSON {
				return output.PrintJSON(map[string]any{
					"total": total, "available": avail, "locked": locked,
				})
			}
			pairs := [][2]string{
				{"total", total},
				{"available", avail},
				{"locked", locked},
			}
			pairs = append(pairs, extra...)
			output.PrintKV(pairs)
			return nil
		},
	}
}

func newCapitalHistoryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "history",
		Short: "Capital history",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := app.From(cmd.Context())
			if err := a.EnsureClient(); err != nil {
				return err
			}
			h, err := a.Client.GetCapitalHistory(cmd.Context())
			if err != nil {
				return err
			}
			if a.JSON {
				return output.PrintJSON(h)
			}
			headers := []string{"Time", "Type", "Token", "Quantity", "Status", "TxHash"}
			rows := make([][]string, 0, len(h.Transactions))
			for _, t := range h.Transactions {
				ts := t.UpdateTime
				if ts == 0 {
					ts = t.TimestampSec
				}
				// formatTS expects ms; seconds-scale needs scaling.
				if ts > 0 && ts < 10_000_000_000 {
					ts *= 1000
				}
				token := t.Token
				if token == "" {
					token = t.Asset
				}
				qty := t.Quantity
				if qty == "" {
					qty = t.Amount
				}
				hash := t.TransactionHash
				if len(hash) > 18 {
					hash = hash[:10] + "..." + hash[len(hash)-6:]
				}
				rows = append(rows, []string{
					formatTS(ts),
					t.TransactionType,
					token,
					qty,
					t.Status,
					hash,
				})
			}
			output.PrintTable(headers, rows, output.NumericAligns(headers, "Quantity"))
			return nil
		},
	}
}

func newCapitalDepositInfoCmd() *cobra.Command {
	var publicKey string
	c := &cobra.Command{
		Use:   "deposit-info",
		Short: "Deposit address info",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := app.From(cmd.Context())
			if err := a.EnsureClient(); err != nil {
				return err
			}
			info, err := a.Client.GetDepositInfo(cmd.Context(), publicKey)
			if err != nil {
				return err
			}
			if a.JSON {
				return output.PrintJSON(info)
			}
			output.PrintKV([][2]string{
				{"coin", info.Coin},
				{"address", info.Address},
			})
			return nil
		},
	}
	c.Flags().StringVar(&publicKey, "public-key", "", "account public key (required)")
	_ = c.MarkFlagRequired("public-key")
	return c
}
