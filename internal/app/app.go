package app

import (
	"context"
	"errors"
	"fmt"

	hibachi "github.com/doedja/hibachi-go"

	"github.com/doedja/hibachi-cli/internal/config"
)

// App is the per-invocation container of shared state. Built once at root
// PreRun and passed to subcommands via context.
type App struct {
	Cfg    *config.Config
	Client *hibachi.Client
	Signer hibachi.Signer

	// Flags set by root command.
	DryRun bool
	Yes    bool
	JSON   bool
}

// ctxKey is unexported to prevent collisions.
type ctxKey struct{}

// Into returns ctx with app attached.
func Into(ctx context.Context, a *App) context.Context {
	return context.WithValue(ctx, ctxKey{}, a)
}

// From retrieves the App from ctx. Panics if absent (programmer error).
func From(ctx context.Context) *App {
	a, ok := ctx.Value(ctxKey{}).(*App)
	if !ok {
		panic("app.From called without app in context")
	}
	return a
}

// Build constructs an App from config. Does not create the client yet;
// call EnsureClient or EnsureSigner when needed. Read-only commands only
// need a client; trade-path commands need both.
func Build(cfg *config.Config) *App {
	return &App{Cfg: cfg}
}

// EnsureClient builds the hibachi.Client lazily. Safe to call repeatedly.
func (a *App) EnsureClient() error {
	if a.Client != nil {
		return nil
	}
	opts := []hibachi.Option{}
	if a.Cfg.API.APIKey != "" {
		opts = append(opts, hibachi.WithAPIKey(a.Cfg.API.APIKey))
	}
	if a.Cfg.API.AccountID != 0 {
		opts = append(opts, hibachi.WithAccountID(a.Cfg.API.AccountID))
	}
	if a.Cfg.API.APIURL != "" {
		opts = append(opts, hibachi.WithAPIURL(a.Cfg.API.APIURL))
	}
	if a.Cfg.API.DataAPIURL != "" {
		opts = append(opts, hibachi.WithDataAPIURL(a.Cfg.API.DataAPIURL))
	}

	pk, err := config.ResolvePrivateKey(a.Cfg)
	if err == nil && pk != "" {
		opts = append(opts, hibachi.WithPrivateKey(pk))
	}

	c, err := hibachi.NewClient(opts...)
	if err != nil {
		return fmt.Errorf("build hibachi client: %w", err)
	}
	a.Client = c
	return nil
}

// EnsureSigner builds the signer lazily. Required for trade paths.
func (a *App) EnsureSigner() error {
	if a.Signer != nil {
		return nil
	}
	pk, err := config.ResolvePrivateKey(a.Cfg)
	if err != nil {
		return err
	}
	if pk == "" {
		return errors.New("no private key configured")
	}
	s, err := hibachi.NewSigner(pk)
	if err != nil {
		return fmt.Errorf("build signer: %w", err)
	}
	a.Signer = s
	return nil
}
