package tpsl

import (
	"context"
	"fmt"

	hibachi "github.com/doedja/hibachi-go"
	"github.com/doedja/hibachi-go/ws"

	"github.com/doedja/hibachi-cli/internal/strategies"
)

// snapshotPositions opens a short-lived account WS, reads the snapshot, and
// returns the current positions. Mirrors cmd/account.fetchAccountSnapshot to
// keep strategies free of cmd-package imports.
func snapshotPositions(ctx context.Context, deps strategies.AgentDeps) ([]hibachi.Position, error) {
	client := ws.NewAccountClient(ws.AccountClientOptions{
		APIKey:    deps.Cfg.API.APIKey,
		AccountID: deps.Cfg.API.AccountID,
	})
	if err := client.Connect(ctx); err != nil {
		return nil, fmt.Errorf("connect account ws: %w", err)
	}
	defer client.Disconnect()
	res, err := client.StreamStart(ctx)
	if err != nil {
		return nil, fmt.Errorf("stream start: %w", err)
	}
	return res.AccountSnapshot.Positions, nil
}
