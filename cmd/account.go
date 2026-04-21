package cmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	hibachi "github.com/doedja/hibachi-go"
	"github.com/doedja/hibachi-go/ws"

	"github.com/doedja/hibachi-cli/internal/app"
	"github.com/doedja/hibachi-cli/internal/output"
)

func newAccountCmd() *cobra.Command {
	c := &cobra.Command{Use: "account", Short: "Account info, positions, orders"}
	c.AddCommand(
		newAccountInfoCmd(),
		newAccountBalanceCmd(),
		newAccountPositionsCmd(),
		newAccountOrdersCmd(),
		newAccountOrderCmd(),
		newAccountTradesCmd(),
	)
	return c
}

func newAccountInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Account summary",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := app.From(cmd.Context())
			if err := a.EnsureClient(); err != nil {
				return err
			}
			info, err := a.Client.GetAccountInfo(cmd.Context())
			if err != nil {
				return err
			}
			if a.JSON {
				return output.PrintJSON(info)
			}
			output.PrintKV([][2]string{
				{"account_id", strconv.Itoa(a.Cfg.API.AccountID)},
				{"balance", info.Balance},
				{"total_position_notional", info.TotalPositionNotional},
			})
			return nil
		},
	}
}

func newAccountBalanceCmd() *cobra.Command {
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

func newAccountPositionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "positions",
		Short: "Open positions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := app.From(cmd.Context())
			if err := a.EnsureClient(); err != nil {
				return err
			}
			// Positions are exposed via the account WebSocket snapshot; there
			// is no REST endpoint. Open a short-lived WS, read the snapshot,
			// then disconnect.
			snap, err := fetchAccountSnapshot(cmd.Context(), a)
			if err != nil {
				return err
			}
			if a.JSON {
				return output.PrintJSON(snap)
			}
			headers := []string{"Symbol", "Dir", "Qty", "Open", "Mark", "Notional", "TradingPnL", "FundingPnL", "Lev"}
			rows := make([][]string, 0, len(snap.Positions))
			for _, p := range snap.Positions {
				rows = append(rows, []string{
					p.Symbol,
					p.Direction,
					p.Quantity,
					p.OpenPrice,
					p.MarkPrice,
					p.NotionalValue,
					output.ColorizePnL(p.UnrealizedTradingPnl),
					output.ColorizePnL(p.UnrealizedFundingPnl),
					strconv.Itoa(p.Leverage),
				})
			}
			aligns := output.NumericAligns(headers, "Qty", "Open", "Mark", "Notional", "TradingPnL", "FundingPnL", "Lev")
			output.PrintTable(headers, rows, aligns)
			return nil
		},
	}
}

func fetchAccountSnapshot(ctx context.Context, a *app.App) (*hibachi.AccountSnapshot, error) {
	if a.Cfg.API.APIKey == "" || a.Cfg.API.AccountID == 0 {
		return nil, fmt.Errorf("account positions require api_key and account_id")
	}
	client := ws.NewAccountClient(ws.AccountClientOptions{
		APIKey:    a.Cfg.API.APIKey,
		AccountID: a.Cfg.API.AccountID,
	})
	if err := client.Connect(ctx); err != nil {
		return nil, fmt.Errorf("connect account ws: %w", err)
	}
	defer client.Disconnect()

	res, err := client.StreamStart(ctx)
	if err != nil {
		return nil, fmt.Errorf("stream start: %w", err)
	}
	return &res.AccountSnapshot, nil
}

func newAccountOrdersCmd() *cobra.Command {
	var pending bool
	c := &cobra.Command{
		Use:   "orders",
		Short: "Orders (defaults to pending)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := app.From(cmd.Context())
			if err := a.EnsureClient(); err != nil {
				return err
			}
			// GetPendingOrders is the only REST orders endpoint available.
			// --pending is kept for clarity; other filters would need API support.
			_ = pending
			orders, err := a.Client.GetPendingOrders(cmd.Context())
			if err != nil {
				return err
			}
			if a.JSON {
				return output.PrintJSON(orders)
			}
			renderOrders(orders)
			return nil
		},
	}
	c.Flags().BoolVar(&pending, "pending", true, "show pending orders only")
	return c
}

func renderOrders(orders []hibachi.Order) {
	headers := []string{"ID", "Symbol", "Side", "Type", "Price", "Qty", "Avail", "Status", "Flags"}
	rows := make([][]string, 0, len(orders))
	for _, o := range orders {
		price := ""
		if o.Price != nil {
			price = *o.Price
		}
		total := ""
		if o.TotalQuantity != nil {
			total = *o.TotalQuantity
		}
		flags := ""
		if o.OrderFlags != nil {
			flags = string(*o.OrderFlags)
		}
		rows = append(rows, []string{
			strconv.FormatInt(o.OrderID, 10),
			o.Symbol,
			string(o.Side),
			string(o.OrderType),
			price,
			total,
			o.AvailableQuantity,
			string(o.Status),
			flags,
		})
	}
	output.PrintTable(headers, rows, output.NumericAligns(headers, "ID", "Price", "Qty", "Avail"))
}

func newAccountOrderCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "order [id]",
		Short: "Order details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.EnsureClient(); err != nil {
				return err
			}
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid order id %q: %w", args[0], err)
			}
			ord, err := a.Client.GetOrderDetails(cmd.Context(), &id, nil)
			if err != nil {
				return err
			}
			if a.JSON {
				return output.PrintJSON(ord)
			}
			pairs := [][2]string{
				{"order_id", strconv.FormatInt(ord.OrderID, 10)},
				{"symbol", ord.Symbol},
				{"side", string(ord.Side)},
				{"type", string(ord.OrderType)},
				{"status", string(ord.Status)},
				{"available_qty", ord.AvailableQuantity},
			}
			if ord.Price != nil {
				pairs = append(pairs, [2]string{"price", *ord.Price})
			}
			if ord.TotalQuantity != nil {
				pairs = append(pairs, [2]string{"total_qty", *ord.TotalQuantity})
			}
			if ord.TriggerPrice != nil {
				pairs = append(pairs, [2]string{"trigger_price", *ord.TriggerPrice})
			}
			if ord.OrderFlags != nil {
				pairs = append(pairs, [2]string{"flags", string(*ord.OrderFlags)})
			}
			if ord.CreationTime != nil {
				pairs = append(pairs, [2]string{"created", formatTS(*ord.CreationTime)})
			}
			if ord.FinishTime != nil {
				pairs = append(pairs, [2]string{"finished", formatTS(*ord.FinishTime)})
			}
			output.PrintKV(pairs)
			return nil
		},
	}
}

func newAccountTradesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "trades",
		Short: "Recent account trades",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := app.From(cmd.Context())
			if err := a.EnsureClient(); err != nil {
				return err
			}
			trades, err := a.Client.GetAccountTrades(cmd.Context())
			if err != nil {
				return err
			}
			if a.JSON {
				return output.PrintJSON(trades)
			}
			headers := []string{"Time", "OrderID", "Symbol", "Side", "Price", "Quantity", "Fee", "PnL"}
			rows := make([][]string, 0, len(trades))
			for _, t := range trades {
				ts := t.Time
				if ts == 0 {
					ts = t.Timestamp
				}
				// Server returns seconds; formatTS detects ms > 10^12.
				if ts > 0 && ts < 10_000_000_000 {
					ts *= 1000
				}
				orderID := t.OrderID
				if orderID == 0 {
					switch strings.ToUpper(string(t.Side)) {
					case "BUY", "BID":
						orderID = t.BidOrderID
					case "SELL", "ASK":
						orderID = t.AskOrderID
					}
				}
				pnl := t.RealizedPnl
				if pnl != "" {
					pnl = output.ColorizePnL(pnl)
				}
				rows = append(rows, []string{
					formatTS(ts),
					strconv.FormatInt(orderID, 10),
					t.Symbol,
					string(t.Side),
					t.Price,
					t.Quantity,
					t.Fee,
					pnl,
				})
			}
			output.PrintTable(headers, rows, output.NumericAligns(headers, "OrderID", "Price", "Quantity", "Fee", "PnL"))
			return nil
		},
	}
}
