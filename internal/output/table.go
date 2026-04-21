package output

import (
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/mattn/go-isatty"
	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
)

// IsTTY reports whether stdout is a terminal.
func IsTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	if (fi.Mode() & os.ModeCharDevice) != 0 {
		return true
	}
	return isatty.IsTerminal(os.Stdout.Fd())
}

// NumericAligns builds a per-column alignment slice where every column name
// in numericCols gets right alignment. Case-insensitive header match.
func NumericAligns(headers []string, numericCols ...string) []tw.Align {
	out := make([]tw.Align, len(headers))
	set := make(map[string]struct{}, len(numericCols))
	for _, n := range numericCols {
		set[strings.ToLower(n)] = struct{}{}
	}
	for i, h := range headers {
		if _, ok := set[strings.ToLower(h)]; ok {
			out[i] = tw.AlignRight
		} else {
			out[i] = tw.AlignLeft
		}
	}
	return out
}

// PrintTable renders rows with a header row. Pass per-column alignments via
// aligns (length must match headers); if nil, all columns align left.
func PrintTable(headers []string, rows [][]string, aligns []tw.Align) {
	if len(aligns) == 0 {
		aligns = make([]tw.Align, len(headers))
		for i := range aligns {
			aligns[i] = tw.AlignLeft
		}
	}
	t := tablewriter.NewTable(os.Stdout,
		tablewriter.WithHeaderAutoFormat(tw.Off),
		tablewriter.WithRowAlignmentConfig(tw.CellAlignment{PerColumn: aligns}),
	)
	t.Header(toAny(headers))
	for _, r := range rows {
		_ = t.Append(toAny(r))
	}
	_ = t.Render()
}

func toAny(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

// PrintKV prints a two-column key/value listing. No header row.
func PrintKV(pairs [][2]string) {
	max := 0
	for _, p := range pairs {
		if n := len(p[0]); n > max {
			max = n
		}
	}
	for _, p := range pairs {
		fmt.Printf("%-*s  %s\n", max, p[0], p[1])
	}
}

// ColorizePnL returns the value colored green (positive) or red (negative) when
// stdout is a TTY. Zero or unparseable values are returned unchanged.
func ColorizePnL(v string) string {
	if !IsTTY() {
		return v
	}
	trimmed := strings.TrimSpace(v)
	if trimmed == "" || trimmed == "0" || trimmed == "0.0" {
		return v
	}
	if strings.HasPrefix(trimmed, "-") {
		return color.New(color.FgRed).Sprint(v)
	}
	// Treat any other numeric-looking value as non-negative.
	if isNumeric(trimmed) {
		return color.New(color.FgGreen).Sprint(v)
	}
	return v
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	dot := false
	start := 0
	if s[0] == '-' || s[0] == '+' {
		start = 1
	}
	if start == len(s) {
		return false
	}
	for _, r := range s[start:] {
		if r == '.' {
			if dot {
				return false
			}
			dot = true
			continue
		}
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
