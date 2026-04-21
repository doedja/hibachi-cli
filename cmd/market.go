package cmd

import (
	"fmt"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	hibachi "github.com/doedja/hibachi-go"

	"github.com/doedja/hibachi-cli/internal/app"
	"github.com/doedja/hibachi-cli/internal/output"
)

func newMarketCmd() *cobra.Command {
	c := &cobra.Command{Use: "market", Short: "Market data"}
	c.AddCommand(
		newMarketInfoCmd(),
		newMarketPriceCmd(),
		newMarketStatsCmd(),
		newMarketOrderbookCmd(),
		newMarketTradesCmd(),
		newMarketKlinesCmd(),
		newMarketOpenInterestCmd(),
	)
	return c
}

func newMarketInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Exchange info and contracts",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := app.From(cmd.Context())
			if err := a.EnsureClient(); err != nil {
				return err
			}
			info, err := a.Client.GetExchangeInfo(cmd.Context())
			if err != nil {
				return err
			}
			if a.JSON {
				return output.PrintJSON(info)
			}
			headers := []string{"Symbol", "ID", "Status", "Tick", "Step", "MinNotional", "UDec", "SDec"}
			rows := make([][]string, 0, len(info.FutureContracts))
			for _, f := range info.FutureContracts {
				rows = append(rows, []string{
					f.Symbol,
					strconv.Itoa(f.ID),
					f.Status,
					f.TickSize,
					f.StepSize,
					f.MinNotional,
					strconv.Itoa(f.UnderlyingDecimals),
					strconv.Itoa(f.SettlementDecimals),
				})
			}
			aligns := output.NumericAligns(headers, "ID", "Tick", "Step", "MinNotional", "UDec", "SDec")
			output.PrintTable(headers, rows, aligns)
			return nil
		},
	}
}

func newMarketPriceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "price [symbol]",
		Short: "Current price",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.EnsureClient(); err != nil {
				return err
			}
			p, err := a.Client.GetPrices(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if a.JSON {
				return output.PrintJSON(p)
			}
			pairs := [][2]string{
				{"symbol", p.Symbol},
				{"mark", p.MarkPrice},
				{"spot", p.SpotPrice},
				{"trade", p.TradePrice},
				{"ask", p.AskPrice},
				{"bid", p.BidPrice},
			}
			if p.FundingRateEstimation != nil {
				pairs = append(pairs,
					[2]string{"funding_rate_est", p.FundingRateEstimation.EstimatedFundingRate},
					[2]string{"next_funding_ts", formatTS(p.FundingRateEstimation.NextFundingTimestamp)},
				)
			}
			output.PrintKV(pairs)
			return nil
		},
	}
}

func newMarketStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats [symbol]",
		Short: "24h stats",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.EnsureClient(); err != nil {
				return err
			}
			s, err := a.Client.GetStats(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if a.JSON {
				return output.PrintJSON(s)
			}
			output.PrintKV([][2]string{
				{"symbol", s.Symbol},
				{"high_24h", s.High24h},
				{"low_24h", s.Low24h},
				{"volume_24h", s.Volume24h},
			})
			return nil
		},
	}
}

func newMarketOrderbookCmd() *cobra.Command {
	var depth, granularity int
	c := &cobra.Command{
		Use:   "orderbook [symbol]",
		Short: "Orderbook snapshot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.EnsureClient(); err != nil {
				return err
			}
			g := granularity
			if g == 0 {
				// Server rejects granularity=0 with a list of valid values per
				// contract. 1 is valid for every current symbol. Pass --granularity
				// explicitly to get finer buckets.
				g = 1
			}
			ob, err := a.Client.GetOrderbook(cmd.Context(), args[0], depth, g)
			if err != nil {
				return err
			}
			if a.JSON {
				return output.PrintJSON(ob)
			}
			fmt.Printf("Bids [%s .. %s]\n", ob.Bid.StartPrice, ob.Bid.EndPrice)
			printLevels(ob.Bid.Levels)
			fmt.Printf("\nAsks [%s .. %s]\n", ob.Ask.StartPrice, ob.Ask.EndPrice)
			printLevels(ob.Ask.Levels)
			return nil
		},
	}
	c.Flags().IntVar(&depth, "depth", 20, "number of price levels")
	c.Flags().IntVar(&granularity, "granularity", 0, "price granularity (contract-defined; 0 defaults to 1)")
	return c
}

func printLevels(levels []hibachi.OrderBookLevel) {
	headers := []string{"Price", "Quantity"}
	rows := make([][]string, len(levels))
	for i, l := range levels {
		rows[i] = []string{l.Price, l.Quantity}
	}
	output.PrintTable(headers, rows, output.NumericAligns(headers, "Price", "Quantity"))
}

func newMarketTradesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "trades [symbol]",
		Short: "Recent trades",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.EnsureClient(); err != nil {
				return err
			}
			t, err := a.Client.GetTrades(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if a.JSON {
				return output.PrintJSON(t)
			}
			headers := []string{"Time", "Price", "Quantity", "TakerSide"}
			rows := make([][]string, 0, len(t.Trades))
			for _, tr := range t.Trades {
				rows = append(rows, []string{
					formatTS(tr.Timestamp),
					tr.Price,
					tr.Quantity,
					string(tr.TakerSide),
				})
			}
			output.PrintTable(headers, rows, output.NumericAligns(headers, "Price", "Quantity"))
			return nil
		},
	}
}

func newMarketKlinesCmd() *cobra.Command {
	var interval string
	c := &cobra.Command{
		Use:   "klines [symbol]",
		Short: "Candles",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.EnsureClient(); err != nil {
				return err
			}
			k, err := a.Client.GetKlines(cmd.Context(), args[0], hibachi.Interval(interval))
			if err != nil {
				return err
			}
			if a.JSON {
				return output.PrintJSON(k)
			}
			headers := []string{"Time", "Interval", "Open", "High", "Low", "Close", "VolumeNotional"}
			rows := make([][]string, 0, len(k.Klines))
			for _, c := range k.Klines {
				rows = append(rows, []string{
					formatTS(c.Timestamp),
					c.Interval,
					c.Open,
					c.High,
					c.Low,
					c.Close,
					c.VolumeNotional,
				})
			}
			output.PrintTable(headers, rows, output.NumericAligns(headers, "Open", "High", "Low", "Close", "VolumeNotional"))
			return nil
		},
	}
	c.Flags().StringVar(&interval, "interval", "1min", "kline interval (1min, 5min, 15min, 1h, 4h, 1d, 1w)")
	return c
}

func newMarketOpenInterestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "open-interest [symbol]",
		Short: "Open interest",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.EnsureClient(); err != nil {
				return err
			}
			oi, err := a.Client.GetOpenInterest(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if a.JSON {
				return output.PrintJSON(oi)
			}
			output.PrintKV([][2]string{
				{"symbol", args[0]},
				{"total_quantity", oi.TotalQuantity},
			})
			return nil
		},
	}
}

// formatTS converts a unix millisecond or second timestamp to UTC RFC3339.
// Exchange returns ms for trades/klines; the function detects by magnitude.
func formatTS(ts int64) string {
	if ts == 0 {
		return ""
	}
	// 10^12 = 2001-09-09 in seconds; anything above is milliseconds.
	var t time.Time
	if ts > 1_000_000_000_000 {
		t = time.UnixMilli(ts).UTC()
	} else {
		t = time.Unix(ts, 0).UTC()
	}
	return t.Format(time.RFC3339)
}
