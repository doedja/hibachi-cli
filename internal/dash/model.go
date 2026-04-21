package dash

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	hibachi "github.com/doedja/hibachi-go"

	"github.com/doedja/hibachi-cli/internal/aiagent"
	"github.com/doedja/hibachi-cli/internal/app"
	"github.com/doedja/hibachi-cli/internal/config"
	"github.com/doedja/hibachi-cli/internal/journal"
	"github.com/doedja/hibachi-cli/internal/memory"
)

// Deps is the set of collaborators the dash needs.
type Deps struct {
	App        *app.App
	Client     *hibachi.Client
	Signer     hibachi.Signer
	Cfg        *config.Config
	Planner    aiagent.Planner
	Journal    *journal.Journal
	Memory     *memory.Store
	InitialSym string
}

// ConnState tracks a WS connection's health.
type ConnState int

const (
	ConnDisabled ConnState = iota
	ConnConnecting
	ConnLive
	ConnReconnecting
	ConnError
)

func (s ConnState) String() string {
	switch s {
	case ConnDisabled:
		return "disabled"
	case ConnConnecting:
		return "connecting"
	case ConnLive:
		return "live"
	case ConnReconnecting:
		return "reconnecting"
	case ConnError:
		return "error"
	}
	return "unknown"
}

// Focus identifies which left-hand panel has keyboard focus.
type Focus int

const (
	FocusPositions Focus = iota
	FocusOrders
	FocusTrades
	FocusWatchlist
	FocusAdvisor
)

// OverlayKind selects which (if any) modal is active.
type OverlayKind int

const (
	OverlayNone OverlayKind = iota
	OverlayPrompt
	OverlayPlanning
	OverlayPlan
	OverlayConfirmClose
	OverlayConfirmCancel
	OverlayHelp
	OverlayError
)

// RecentTrade is a ring-buffer entry.
type RecentTrade struct {
	Symbol    string
	Price     string
	Quantity  string
	Taker     string
	Timestamp int64
}

// Model is the bubbletea model for the dash.
type Model struct {
	deps       Deps
	ctx        context.Context
	feedCancel context.CancelFunc
	styles     Styles
	keys       KeyMap
	width      int
	height     int
	now        time.Time
	advisorOn  bool

	focusedSymbol string
	focus         Focus

	marketStatus  ConnState
	accountStatus ConnState
	statusReason  string

	// Market data.
	orderbook    *hibachi.OrderBook
	lastOBAt     time.Time
	recentTrades []RecentTrade
	maxTrades    int
	watchlist    []string
	watchPrices  map[string]PriceTickMsg
	watchHistory map[string][]float64

	// Account data.
	accountInfo   *hibachi.AccountInfo
	snapshot      *hibachi.AccountSnapshot
	pendingOrders []hibachi.Order

	// Selection (position / order / watchlist).
	positionIdx int
	orderIdx    int
	watchIdx    int

	// Overlay state.
	overlay      OverlayKind
	prompt       string
	lastPlan     *aiagent.Plan
	lastResp     *aiagent.Response
	lastPrompt   string
	banner       string
	bannerUntil  time.Time
	lastError    string
	advisor      *AdvisorTickMsg
	lastAdvisorTS time.Time

	// Metadata.
	contractsBySymbol map[string]hibachi.FutureContract
}

// New constructs a Model.
func New(deps Deps) Model {
	watch := append([]string(nil), DefaultWatchlist()...)
	if deps.InitialSym != "" && !contains(watch, deps.InitialSym) {
		watch = append([]string{deps.InitialSym}, watch...)
	}
	return Model{
		deps:              deps,
		styles:            DefaultStyles(),
		keys:              DefaultKeyMap(),
		focusedSymbol:     deps.InitialSym,
		focus:             FocusPositions,
		marketStatus:      ConnConnecting,
		accountStatus:     initialAccountStatus(deps),
		advisorOn:         true,
		maxTrades:         40,
		watchlist:         watch,
		watchPrices:       map[string]PriceTickMsg{},
		watchHistory:      map[string][]float64{},
		now:               time.Now().UTC(),
		contractsBySymbol: map[string]hibachi.FutureContract{},
	}
}

func initialAccountStatus(deps Deps) ConnState {
	if deps.Cfg == nil || deps.Cfg.API.APIKey == "" || deps.Cfg.API.AccountID == 0 {
		return ConnDisabled
	}
	return ConnConnecting
}

// DefaultWatchlist returns the v0.1 hardcoded list (no config key yet).
func DefaultWatchlist() []string {
	return []string{"BTC/USDT-P", "ETH/USDT-P", "SOL/USDT-P"}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// Run runs the dash and blocks until the user quits.
func Run(ctx context.Context, deps Deps) error {
	m := New(deps)

	prog := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())

	// Start background feeds that push tea.Msgs into the program.
	feedCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	startFeeds(feedCtx, deps, func(msg tea.Msg) { prog.Send(msg) })

	_, err := prog.Run()
	return err
}

// Init returns the initial commands.
func (m Model) Init() tea.Cmd {
	return tea.Batch(tickCmd(), loadContractsCmd(m.deps))
}
