package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/zalando/go-keyring"
)

const (
	keyringService = "hibachi-cli"
)

type Config struct {
	API      APIConfig        `toml:"api"`
	AI       AIConfig         `toml:"ai"`
	Safety   SafetyConfig     `toml:"safety"`
	Journal  JournalConfig    `toml:"journal"`
	Memory   MemoryConfig     `toml:"memory"`
}

type APIConfig struct {
	AccountID      int    `toml:"account_id"`
	APIKey         string `toml:"api_key"`
	APIURL         string `toml:"api_url"`
	DataAPIURL     string `toml:"data_api_url"`
	PrivateKey     string `toml:"private_key"`
	PrivateKeyEnv  string `toml:"private_key_env"`
	PrivateKeyRing bool   `toml:"private_key_ring"`
}

type AIConfig struct {
	Backend     string             `toml:"backend"`
	ClaudeCode  ClaudeCodeConfig   `toml:"claude_code"`
	OpenRouter  OpenRouterConfig   `toml:"openrouter"`
}

type ClaudeCodeConfig struct {
	Bin        string `toml:"bin"`
	Model      string `toml:"model"`
	TimeoutSec int    `toml:"timeout_sec"`
}

type OpenRouterConfig struct {
	BaseURL     string  `toml:"base_url"`
	Model       string  `toml:"model"`
	APIKeyEnv   string  `toml:"api_key_env"`
	TimeoutSec  int     `toml:"timeout_sec"`
	Temperature float64 `toml:"temperature"`
	MaxTokens   int     `toml:"max_tokens"`
}

type SafetyConfig struct {
	MaxNotionalUSD float64  `toml:"max_notional_usd"`
	Symbols        []string `toml:"symbols"`
	RequireConfirm bool     `toml:"require_confirm"`
	DryRunDefault  bool     `toml:"dry_run_default"`
}

type JournalConfig struct {
	Path string `toml:"path"`
}

type MemoryConfig struct {
	Dir             string `toml:"dir"`
	SoftTokens      int    `toml:"soft_tokens"`
	HardTokens      int    `toml:"hard_tokens"`
}

func defaults() Config {
	home, _ := os.UserHomeDir()
	return Config{
		AI: AIConfig{
			Backend: "claude-code",
			ClaudeCode: ClaudeCodeConfig{
				Bin:        "claude",
				Model:      "claude-opus-4-7",
				TimeoutSec: 120,
			},
			OpenRouter: OpenRouterConfig{
				BaseURL:    "https://openrouter.ai/api/v1",
				Model:      "anthropic/claude-opus-4.7",
				APIKeyEnv:  "OPENROUTER_API_KEY",
				TimeoutSec: 90,
			},
		},
		Safety: SafetyConfig{
			MaxNotionalUSD: 100,
			RequireConfirm: true,
			DryRunDefault:  true,
		},
		Journal: JournalConfig{
			Path: filepath.Join(home, ".hibachi", "journal.db"),
		},
		Memory: MemoryConfig{
			Dir:        filepath.Join(home, ".hibachi", "memory"),
			SoftTokens: 8000,
			HardTokens: 32000,
		},
	}
}

// DefaultPath returns the config path, honoring HIBACHI_CONFIG env.
func DefaultPath() string {
	if p := os.Getenv("HIBACHI_CONFIG"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "hibachi", "config.toml")
}

// Load reads config from path (TOML), applies env overrides, returns Config.
// Missing file is not an error; defaults + env are used.
func Load(path string) (*Config, error) {
	cfg := defaults()

	if path != "" {
		data, err := os.ReadFile(path)
		if err == nil {
			if err := toml.Unmarshal(data, &cfg); err != nil {
				return nil, fmt.Errorf("parse config %s: %w", path, err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read config %s: %w", path, err)
		}
	}

	applyEnv(&cfg)
	expandPaths(&cfg)
	return &cfg, nil
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("HIBACHI_ACCOUNT_ID"); v != "" {
		fmt.Sscanf(v, "%d", &cfg.API.AccountID)
	}
	if v := os.Getenv("HIBACHI_API_KEY"); v != "" {
		cfg.API.APIKey = v
	}
	if v := os.Getenv("HIBACHI_API_URL"); v != "" {
		cfg.API.APIURL = v
	}
	if v := os.Getenv("HIBACHI_DATA_API_URL"); v != "" {
		cfg.API.DataAPIURL = v
	}
	// HIBACHI_PRIVATE_KEY is intentionally not copied into cfg.API.PrivateKey.
	// ResolvePrivateKey reads the env directly so `auth status` can label the
	// source accurately.
}

func expandPaths(cfg *Config) {
	cfg.Journal.Path = expand(cfg.Journal.Path)
	cfg.Memory.Dir = expand(cfg.Memory.Dir)
}

func expand(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}

// PrivateKeySource labels where ResolvePrivateKey found the key.
type PrivateKeySource string

const (
	PKSourceUnset    PrivateKeySource = "(unset)"
	PKSourceEnv      PrivateKeySource = "env HIBACHI_PRIVATE_KEY"
	PKSourceConfig   PrivateKeySource = "config.api.private_key"
	PKSourceCustom   PrivateKeySource = "env (custom)"
	PKSourceKeychain PrivateKeySource = "keychain"
)

// ResolvePrivateKey returns the effective private key, preferring (in order):
// HIBACHI_PRIVATE_KEY env, config.api.private_key, custom env var name,
// keychain entry under the account ID. Env wins because an explicit export
// should override a saved config for testing other accounts.
func ResolvePrivateKey(cfg *Config) (string, error) {
	k, _, err := ResolvePrivateKeyWithSource(cfg)
	return k, err
}

// ResolvePrivateKeyWithSource returns the key and a label for where it came
// from. The label is for display only.
func ResolvePrivateKeyWithSource(cfg *Config) (string, PrivateKeySource, error) {
	if v := os.Getenv("HIBACHI_PRIVATE_KEY"); v != "" {
		return v, PKSourceEnv, nil
	}
	if cfg.API.PrivateKey != "" {
		return cfg.API.PrivateKey, PKSourceConfig, nil
	}
	if cfg.API.PrivateKeyEnv != "" {
		if v := os.Getenv(cfg.API.PrivateKeyEnv); v != "" {
			return v, PrivateKeySource("env " + cfg.API.PrivateKeyEnv), nil
		}
	}
	if cfg.API.PrivateKeyRing && cfg.API.AccountID != 0 {
		k, err := keyring.Get(keyringService, keyringUser(cfg.API.AccountID))
		if err != nil {
			return "", PKSourceUnset, fmt.Errorf("keyring: %w", err)
		}
		return k, PKSourceKeychain, nil
	}
	return "", PKSourceUnset, errors.New("no private key configured (set config.api.private_key, HIBACHI_PRIVATE_KEY, or enable keyring)")
}

// SetPrivateKeyInRing stores the private key in the OS keychain keyed by account ID.
func SetPrivateKeyInRing(accountID int, privateKey string) error {
	return keyring.Set(keyringService, keyringUser(accountID), privateKey)
}

// DeletePrivateKeyInRing removes the keychain entry for the account.
func DeletePrivateKeyInRing(accountID int) error {
	return keyring.Delete(keyringService, keyringUser(accountID))
}

func keyringUser(accountID int) string {
	return fmt.Sprintf("account-%d", accountID)
}

// Save writes the config back to disk as TOML, creating the parent dir if needed.
func Save(path string, cfg *Config) error {
	if path == "" {
		path = DefaultPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
