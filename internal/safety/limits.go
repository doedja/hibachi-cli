// Package safety holds the pre-trade checks (notional cap, symbol whitelist)
// and the interactive confirm gate. Kept free of SDK types so AI plans can
// be pre-checked before any order object is built.
package safety

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

type Limits struct {
	MaxNotionalUSD float64
	Symbols        []string
	RequireConfirm bool
	DryRun         bool
}

type Action struct {
	Kind        string
	Symbol      string
	NotionalUSD float64
}

func (l Limits) Check(a Action) error {
	if a.Kind == "" {
		return errors.New("safety: action kind is empty")
	}
	// Cancel paths identify by order id or nonce, not symbol; they bypass
	// symbol/notional checks entirely.
	if isCancel(a.Kind) {
		return nil
	}
	if isTrade(a.Kind) {
		if a.Symbol == "" {
			return fmt.Errorf("safety: %s action missing symbol", a.Kind)
		}
		if !l.symbolAllowed(a.Symbol) {
			return fmt.Errorf("safety: symbol %s not in whitelist %v", a.Symbol, l.Symbols)
		}
		if l.MaxNotionalUSD > 0 && a.NotionalUSD > l.MaxNotionalUSD {
			return fmt.Errorf("safety: notional %.2f USD exceeds cap %.2f USD", a.NotionalUSD, l.MaxNotionalUSD)
		}
	}
	return nil
}

func isCancel(kind string) bool {
	return kind == "trade.cancel" || kind == "trade.cancel_all"
}

func (l Limits) symbolAllowed(symbol string) bool {
	if len(l.Symbols) == 0 {
		return true
	}
	for _, s := range l.Symbols {
		if s == symbol {
			return true
		}
	}
	return false
}

// isTrade reports whether the action kind hits the exchange and therefore
// needs symbol/notional checking. Cancel paths pass through (notional may be 0).
func isTrade(kind string) bool {
	return strings.HasPrefix(kind, "trade.") || strings.HasPrefix(kind, "capital.")
}

// Confirm writes preview to w and reads a yes/no from r. Returns true on yes.
// skipYes=true returns true without reading (caller decides when that's safe,
// usually when --yes is set or RequireConfirm is false in config).
func Confirm(w io.Writer, r io.Reader, preview string, skipYes bool) (bool, error) {
	if skipYes {
		return true, nil
	}
	if _, err := fmt.Fprintln(w, preview); err != nil {
		return false, err
	}
	if _, err := fmt.Fprint(w, "Proceed? [y/N]: "); err != nil {
		return false, err
	}
	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return false, err
		}
		return false, nil
	}
	ans := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return ans == "y" || ans == "yes", nil
}
