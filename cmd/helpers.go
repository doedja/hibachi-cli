package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
	"github.com/spf13/cobra"

	hibachi "github.com/doedja/hibachi-go"

	"github.com/doedja/hibachi-cli/internal/app"
	"github.com/doedja/hibachi-cli/internal/safety"
)

// fetchBalance returns (total, available, locked, extra) best-effort.
// The /capital/balance endpoint on Hibachi sometimes returns empty strings;
// when that happens we fall back to /trade/account/info which always has
// `balance` populated. "extra" is keyed display pairs for richer output.
func fetchBalance(ctx context.Context, a *app.App) (total, available, locked string, extra [][2]string, err error) {
	cb, cerr := a.Client.GetCapitalBalance(ctx)
	if cerr == nil && cb != nil && (cb.TotalBalance != "" || cb.AvailableBalance != "") {
		return cb.TotalBalance, cb.AvailableBalance, cb.LockedBalance, nil, nil
	}
	info, aerr := a.Client.GetAccountInfo(ctx)
	if aerr != nil {
		if cerr != nil {
			return "", "", "", nil, fmt.Errorf("capital balance and account info both failed: capital=%v account=%v", cerr, aerr)
		}
		return "", "", "", nil, aerr
	}
	extra = append(extra, [2]string{"source", "account_info (capital endpoint empty)"})
	if info.TotalPositionNotional != "" {
		extra = append(extra, [2]string{"total_position_notional", info.TotalPositionNotional})
	}
	return info.Balance, info.Balance, "0", extra, nil
}

// notImplemented is a placeholder for subcommands still being wired.
func notImplemented(_ *cobra.Command, _ []string) error {
	return errors.New("not implemented")
}

// safetyLimits maps app config into safety.Limits for pre-trade checks.
func safetyLimits(a *app.App) safety.Limits {
	return safety.Limits{
		MaxNotionalUSD: a.Cfg.Safety.MaxNotionalUSD,
		Symbols:        a.Cfg.Safety.Symbols,
		RequireConfirm: a.Cfg.Safety.RequireConfirm,
		DryRun:         a.DryRun,
	}
}

// parseSide normalizes a user-provided side string into a hibachi.Side value.
// Accepts BUY/BID for long and SELL/ASK for short. Case-insensitive.
func parseSide(s string) (hibachi.Side, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "BUY", "BID":
		return hibachi.SideBid, nil
	case "SELL", "ASK":
		return hibachi.SideAsk, nil
	default:
		return "", fmt.Errorf("invalid side %q (expected BUY, SELL, BID, or ASK)", s)
	}
}

// parseDecimal wraps decimal.NewFromString with a clearer error message.
func parseDecimal(s string) (decimal.Decimal, error) {
	d, err := decimal.NewFromString(strings.TrimSpace(s))
	if err != nil {
		return decimal.Decimal{}, fmt.Errorf("invalid decimal %q: %w", s, err)
	}
	return d, nil
}
