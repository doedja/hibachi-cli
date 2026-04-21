package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	hibachi "github.com/doedja/hibachi-go"
	"github.com/doedja/hibachi-go/ws"

	"github.com/doedja/hibachi-cli/internal/app"
)

func newStreamCmd() *cobra.Command {
	c := &cobra.Command{Use: "stream", Short: "Live websocket streams"}
	c.AddCommand(
		newStreamOrderbookCmd(),
		newStreamTradesCmd(),
		newStreamKlinesCmd(),
		newStreamAccountCmd(),
	)
	return c
}

func streamSignalCtx(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
}

func newStreamOrderbookCmd() *cobra.Command {
	var depth int
	var granularity string
	c := &cobra.Command{
		Use:   "orderbook [symbol]",
		Short: "Stream orderbook updates",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			ctx, stop := streamSignalCtx(cmd.Context())
			defer stop()

			symbol := args[0]
			client := ws.NewMarketClient(ws.MarketClientOptions{})
			if err := client.Connect(ctx); err != nil {
				return fmt.Errorf("connect market ws: %w", err)
			}
			defer client.Disconnect()

			client.OnReconnect(func() {
				fmt.Println("[reconnected]")
			})
			client.OnDisconnect(func(err error) {
				fmt.Printf("[disconnected: %v]\n", err)
			})

			client.On(string(hibachi.WSTopicOrderbook), func(data json.RawMessage) {
				if a.JSON {
					fmt.Println(string(data))
					return
				}
				printOrderbookEvent(symbol, data)
			})

			g := granularity
			if g == "" {
				g = "1"
			}
			sub := hibachi.WSSubscription{
				Topic:       hibachi.WSTopicOrderbook,
				Symbol:      symbol,
				Depth:       &depth,
				Granularity: &g,
			}
			if err := client.Subscribe(ctx, sub); err != nil {
				return fmt.Errorf("subscribe orderbook: %w", err)
			}

			return waitMarket(ctx, client)
		},
	}
	c.Flags().IntVar(&depth, "depth", 10, "number of price levels")
	c.Flags().StringVar(&granularity, "granularity", "1", "price granularity as string (e.g. 1, 10, 0.1)")
	return c
}

func newStreamTradesCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "trades [symbol]",
		Short: "Stream trades",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			ctx, stop := streamSignalCtx(cmd.Context())
			defer stop()

			symbol := args[0]
			client := ws.NewMarketClient(ws.MarketClientOptions{})
			if err := client.Connect(ctx); err != nil {
				return fmt.Errorf("connect market ws: %w", err)
			}
			defer client.Disconnect()

			client.OnReconnect(func() {
				fmt.Println("[reconnected]")
			})
			client.OnDisconnect(func(err error) {
				fmt.Printf("[disconnected: %v]\n", err)
			})

			client.On(string(hibachi.WSTopicTrades), func(data json.RawMessage) {
				if a.JSON {
					fmt.Println(string(data))
					return
				}
				printTradeEvent(symbol, data)
			})

			sub := hibachi.WSSubscription{Topic: hibachi.WSTopicTrades, Symbol: symbol}
			if err := client.Subscribe(ctx, sub); err != nil {
				return fmt.Errorf("subscribe trades: %w", err)
			}

			return waitMarket(ctx, client)
		},
	}
	return c
}

func newStreamKlinesCmd() *cobra.Command {
	var interval string
	c := &cobra.Command{
		Use:   "klines [symbol]",
		Short: "Stream klines",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			ctx, stop := streamSignalCtx(cmd.Context())
			defer stop()

			symbol := args[0]
			iv := hibachi.Interval(interval)
			client := ws.NewMarketClient(ws.MarketClientOptions{})
			if err := client.Connect(ctx); err != nil {
				return fmt.Errorf("connect market ws: %w", err)
			}
			defer client.Disconnect()

			client.OnReconnect(func() {
				fmt.Println("[reconnected]")
			})
			client.OnDisconnect(func(err error) {
				fmt.Printf("[disconnected: %v]\n", err)
			})

			client.On(string(hibachi.WSTopicKlines), func(data json.RawMessage) {
				if a.JSON {
					fmt.Println(string(data))
					return
				}
				printKlineEvent(symbol, data)
			})

			sub := hibachi.WSSubscription{
				Topic:    hibachi.WSTopicKlines,
				Symbol:   symbol,
				Interval: &iv,
			}
			if err := client.Subscribe(ctx, sub); err != nil {
				return fmt.Errorf("subscribe klines: %w", err)
			}

			return waitMarket(ctx, client)
		},
	}
	c.Flags().StringVar(&interval, "interval", "1min", "kline interval (1min, 5min, 15min, 1h, 4h, 1d, 1w)")
	return c
}

func newStreamAccountCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "account",
		Short: "Stream account updates",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := app.From(cmd.Context())
			if a.Cfg.API.APIKey == "" || a.Cfg.API.AccountID == 0 {
				return fmt.Errorf("account stream requires api_key and account_id")
			}
			ctx, stop := streamSignalCtx(cmd.Context())
			defer stop()

			client := ws.NewAccountClient(ws.AccountClientOptions{
				APIKey:    a.Cfg.API.APIKey,
				AccountID: a.Cfg.API.AccountID,
			})
			if err := client.Connect(ctx); err != nil {
				return fmt.Errorf("connect account ws: %w", err)
			}
			defer client.Disconnect()

			client.OnReconnect(func(res *hibachi.AccountStreamStartResult) {
				fmt.Println("[reconnected]")
				if res != nil {
					printAccountSnapshot(&res.AccountSnapshot, a.Cfg.API.AccountID)
				}
			})
			client.OnDisconnect(func(err error) {
				fmt.Printf("[disconnected: %v]\n", err)
			})

			res, err := client.StreamStart(ctx)
			if err != nil {
				return fmt.Errorf("stream start: %w", err)
			}

			if a.JSON {
				if err := json.NewEncoder(os.Stdout).Encode(res); err != nil {
					return err
				}
			} else {
				printAccountSnapshot(&res.AccountSnapshot, a.Cfg.API.AccountID)
			}

			client.OnAll(func(topic string, raw json.RawMessage) {
				if topic == "" {
					return
				}
				if a.JSON {
					fmt.Println(string(raw))
					return
				}
				printAccountEvent(topic, raw)
			})

			if err := client.ListenLoop(ctx); err != nil && ctx.Err() == nil {
				return err
			}
			return nil
		},
	}
}

func waitMarket(ctx context.Context, client *ws.MarketClient) error {
	select {
	case err := <-client.Done():
		if err != nil && ctx.Err() == nil {
			return err
		}
		return nil
	case <-ctx.Done():
		return nil
	}
}

// printOrderbookEvent renders a compact snapshot: mid, best bid/ask, top 3 levels.
func printOrderbookEvent(symbol string, data json.RawMessage) {
	var ob hibachi.OrderBook
	if err := json.Unmarshal(data, &ob); err != nil {
		fmt.Fprintf(os.Stderr, "parse error: %v\n", err)
		return
	}
	ts := time.Now().UTC().Format("15:04:05.000")
	bestBid, bestAsk := "", ""
	if len(ob.Bid.Levels) > 0 {
		bestBid = ob.Bid.Levels[0].Price
	}
	if len(ob.Ask.Levels) > 0 {
		bestAsk = ob.Ask.Levels[0].Price
	}
	mid := midPrice(bestBid, bestAsk)
	fmt.Printf("[%s] %s mid=%s bid=%s ask=%s\n", ts, symbol, mid, bestBid, bestAsk)
	topN := 3
	fmt.Println("  bids:")
	for i, l := range ob.Bid.Levels {
		if i >= topN {
			break
		}
		fmt.Printf("    %s  %s\n", l.Price, l.Quantity)
	}
	fmt.Println("  asks:")
	for i, l := range ob.Ask.Levels {
		if i >= topN {
			break
		}
		fmt.Printf("    %s  %s\n", l.Price, l.Quantity)
	}
}

func midPrice(bid, ask string) string {
	b, errB := strconv.ParseFloat(bid, 64)
	a, errA := strconv.ParseFloat(ask, 64)
	if errB != nil || errA != nil || b == 0 || a == 0 {
		return ""
	}
	return strconv.FormatFloat((b+a)/2, 'f', -1, 64)
}

// printTradeEvent supports three observed shapes:
//   {"trade":{...}} (single event, the live stream shape)
//   {"trades":[...]} (TradesResponse)
//   {price,quantity,timestamp,takerSide} (bare Trade)
func printTradeEvent(symbol string, data json.RawMessage) {
	var wrapper struct {
		Trade *hibachi.Trade `json:"trade"`
	}
	if err := json.Unmarshal(data, &wrapper); err == nil && wrapper.Trade != nil {
		printOneTrade(symbol, *wrapper.Trade)
		return
	}
	var list hibachi.TradesResponse
	if err := json.Unmarshal(data, &list); err == nil && len(list.Trades) > 0 {
		for _, t := range list.Trades {
			printOneTrade(symbol, t)
		}
		return
	}
	var single hibachi.Trade
	if err := json.Unmarshal(data, &single); err != nil {
		fmt.Fprintf(os.Stderr, "parse error: %v\n", err)
		return
	}
	printOneTrade(symbol, single)
}

func printOneTrade(symbol string, t hibachi.Trade) {
	side := string(t.TakerSide)
	colored := side
	if colored == "" {
		colored = "?"
	}
	switch side {
	case string(hibachi.TakerSideBuy), "BUY":
		colored = color.New(color.FgGreen).Sprint("BUY")
	case string(hibachi.TakerSideSell), "SELL":
		colored = color.New(color.FgRed).Sprint("SELL")
	}
	fmt.Printf("[%s] %s %s qty=%s px=%s\n",
		formatStreamTS(t.Timestamp),
		symbol,
		colored,
		t.Quantity,
		t.Price,
	)
}

// printKlineEvent supports a single Kline or a KlinesResponse.
func printKlineEvent(symbol string, data json.RawMessage) {
	var list hibachi.KlinesResponse
	if err := json.Unmarshal(data, &list); err == nil && len(list.Klines) > 0 {
		for _, k := range list.Klines {
			printOneKline(symbol, k)
		}
		return
	}
	var k hibachi.Kline
	if err := json.Unmarshal(data, &k); err != nil {
		fmt.Fprintf(os.Stderr, "parse error: %v\n", err)
		return
	}
	printOneKline(symbol, k)
}

func printOneKline(symbol string, k hibachi.Kline) {
	fmt.Printf("[%s] %s %s open=%s high=%s low=%s close=%s vol=%s\n",
		formatStreamTS(k.Timestamp),
		symbol,
		k.Interval,
		k.Open,
		k.High,
		k.Low,
		k.Close,
		k.VolumeNotional,
	)
}

func printAccountSnapshot(snap *hibachi.AccountSnapshot, accountIDFallback int) {
	id := snap.AccountID
	if id == 0 {
		id = accountIDFallback
	}
	fmt.Printf("account %d balance=%s positions=%d\n",
		id,
		snap.Balance,
		len(snap.Positions),
	)
	for _, p := range snap.Positions {
		fmt.Printf("  pos %s %s qty=%s open=%s mark=%s\n",
			p.Symbol, p.Direction, p.Quantity, p.OpenPrice, p.MarkPrice,
		)
	}
}

func printAccountEvent(topic string, raw json.RawMessage) {
	ts := time.Now().UTC().Format("15:04:05.000")
	var env struct {
		Event string          `json:"event"`
		Data  json.RawMessage `json:"data"`
	}
	_ = json.Unmarshal(raw, &env)
	payload := env.Data
	if len(payload) == 0 {
		payload = raw
	}
	fmt.Printf("[%s] %s: %s\n", ts, topic, summarizeAccountPayload(topic, payload))
}

func summarizeAccountPayload(topic string, data json.RawMessage) string {
	switch topic {
	case "balance":
		var b struct {
			Balance          string `json:"balance"`
			AvailableBalance string `json:"availableBalance"`
		}
		if err := json.Unmarshal(data, &b); err == nil {
			if b.Balance != "" || b.AvailableBalance != "" {
				return fmt.Sprintf("balance=%s available=%s", b.Balance, b.AvailableBalance)
			}
		}
	case "position":
		var p hibachi.Position
		if err := json.Unmarshal(data, &p); err == nil && p.Symbol != "" {
			return fmt.Sprintf("%s %s qty=%s mark=%s pnl=%s",
				p.Symbol, p.Direction, p.Quantity, p.MarkPrice, p.UnrealizedTradingPnl)
		}
	case "order":
		var o hibachi.Order
		if err := json.Unmarshal(data, &o); err == nil && o.Symbol != "" {
			price := ""
			if o.Price != nil {
				price = *o.Price
			}
			return fmt.Sprintf("id=%d %s %s %s status=%s px=%s avail=%s",
				o.OrderID, o.Symbol, string(o.Side), string(o.OrderType),
				string(o.Status), price, o.AvailableQuantity)
		}
	case "trade":
		var t hibachi.AccountTrade
		if err := json.Unmarshal(data, &t); err == nil && t.Symbol != "" {
			return fmt.Sprintf("%s %s qty=%s px=%s", t.Symbol, string(t.Side), t.Quantity, t.Price)
		}
	}
	return string(data)
}

// formatStreamTS is like formatTS in market.go but more compact for streams.
func formatStreamTS(ts int64) string {
	if ts == 0 {
		return time.Now().UTC().Format("15:04:05.000")
	}
	var t time.Time
	if ts > 1_000_000_000_000 {
		t = time.UnixMilli(ts).UTC()
	} else {
		t = time.Unix(ts, 0).UTC()
	}
	return t.Format("15:04:05.000")
}
