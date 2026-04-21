package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/doedja/hibachi-cli/internal/app"
	"github.com/doedja/hibachi-cli/internal/config"
)

func newAuthCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "auth",
		Short: "Manage credentials",
	}
	c.AddCommand(
		&cobra.Command{Use: "login", Short: "Interactive login (stores private key in keychain)", RunE: runAuthLogin},
		&cobra.Command{Use: "status", Short: "Show current credential status", RunE: runAuthStatus},
		&cobra.Command{Use: "logout", Short: "Clear stored private key from keychain", RunE: runAuthLogout},
	)
	return c
}

func runAuthLogin(cmd *cobra.Command, _ []string) error {
	a := app.From(cmd.Context())
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("No Hibachi account yet? Sign up at https://hibachi.xyz/r/hoshii")
	fmt.Println()
	fmt.Print("Account ID: ")
	accStr, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read account id: %w", err)
	}
	accStr = strings.TrimSpace(accStr)
	accID, err := strconv.Atoi(accStr)
	if err != nil || accID <= 0 {
		return fmt.Errorf("invalid account id: %q", accStr)
	}

	fmt.Print("API key: ")
	apiKey, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read api key: %w", err)
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return errors.New("api key is required")
	}

	fmt.Print("Private key (input hidden): ")
	pkBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return fmt.Errorf("read private key: %w", err)
	}
	pk := strings.TrimSpace(string(pkBytes))
	if pk == "" {
		return errors.New("private key is required")
	}

	// Persist config with keyring reference (do not write private key to disk).
	cfg := a.Cfg
	cfg.API.AccountID = accID
	cfg.API.APIKey = apiKey
	cfg.API.PrivateKey = ""
	cfg.API.PrivateKeyRing = true

	if err := config.SetPrivateKeyInRing(accID, pk); err != nil {
		return fmt.Errorf("store private key in keychain: %w", err)
	}

	path := config.DefaultPath()
	if err := config.Save(path, cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Printf("Saved config to %s\n", path)
	fmt.Println("Private key stored in OS keychain.")
	return nil
}

func runAuthStatus(cmd *cobra.Command, _ []string) error {
	a := app.From(cmd.Context())
	cfg := a.Cfg

	pairs := [][2]string{
		{"config", config.DefaultPath()},
		{"account_id", fmt.Sprintf("%d", cfg.API.AccountID)},
		{"api_key", presence(cfg.API.APIKey)},
		{"api_url", defaultIfEmpty(cfg.API.APIURL, "(default)")},
		{"data_api_url", defaultIfEmpty(cfg.API.DataAPIURL, "(default)")},
		{"private_key_source", pkSource(cfg)},
	}
	for _, p := range pairs {
		fmt.Printf("%-20s %s\n", p[0], p[1])
	}

	if cfg.API.AccountID == 0 && cfg.API.APIKey == "" {
		fmt.Println()
		fmt.Println("No credentials configured. Run `hibachi auth login` to set up.")
		fmt.Println("No account yet? Sign up at https://hibachi.xyz/r/hoshii")
	}
	return nil
}

func runAuthLogout(cmd *cobra.Command, _ []string) error {
	a := app.From(cmd.Context())
	id := a.Cfg.API.AccountID
	if id == 0 {
		return errors.New("no account id configured")
	}
	if err := config.DeletePrivateKeyInRing(id); err != nil {
		return fmt.Errorf("delete keychain entry: %w", err)
	}
	fmt.Printf("Removed keychain entry for account %d\n", id)
	return nil
}

func presence(s string) string {
	if s == "" {
		return "(unset)"
	}
	return "set"
}

func defaultIfEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func pkSource(cfg *config.Config) string {
	_, src, _ := config.ResolvePrivateKeyWithSource(cfg)
	return string(src)
}
