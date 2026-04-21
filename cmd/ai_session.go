package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

type sessionEntry struct {
	Name      string    `json:"name"`
	SessionID string    `json:"session_id"`
	Backend   string    `json:"backend"`
	Model     string    `json:"model"`
	CreatedAt time.Time `json:"created_at"`
	LastUsed  time.Time `json:"last_used"`
	Turns     int       `json:"turns"`
	CostUSD   float64   `json:"cost_usd"`
}

func sessionsPath(journalPath string) string {
	return filepath.Join(filepath.Dir(journalPath), "sessions.json")
}

func loadSessions(path string) (map[string]*sessionEntry, error) {
	out := map[string]*sessionEntry{}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return out, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func saveSessions(path string, m map[string]*sessionEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
