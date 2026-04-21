// Package journal is an append-only SQLite log of CLI and AI actions.
// Rows are never updated except via SetOutcome, which fills in the result
// of a previously-recorded intent.
package journal

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    ts         TIMESTAMP NOT NULL,
    kind       TEXT NOT NULL,
    symbol     TEXT,
    agent      TEXT,
    payload    TEXT NOT NULL,
    outcome    TEXT
);
CREATE INDEX IF NOT EXISTS events_ts_idx ON events(ts);
CREATE INDEX IF NOT EXISTS events_symbol_idx ON events(symbol);
CREATE INDEX IF NOT EXISTS events_kind_idx ON events(kind);
`

type Journal struct {
	db *sql.DB
}

type Event struct {
	ID        int64           `json:"id"`
	Timestamp time.Time       `json:"ts"`
	Kind      string          `json:"kind"`
	Symbol    string          `json:"symbol,omitempty"`
	Agent     string          `json:"agent,omitempty"`
	Payload   json.RawMessage `json:"payload"`
	Outcome   json.RawMessage `json:"outcome,omitempty"`
}

func Open(path string) (*Journal, error) {
	if path == "" {
		return nil, fmt.Errorf("journal: empty path")
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("journal: mkdir %s: %w", dir, err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("journal: open %s: %w", path, err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("journal: init schema: %w", err)
	}
	return &Journal{db: db}, nil
}

func (j *Journal) Close() error {
	if j == nil || j.db == nil {
		return nil
	}
	return j.db.Close()
}

func (j *Journal) Record(ctx context.Context, e Event) (int64, error) {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	if len(e.Payload) == 0 {
		e.Payload = json.RawMessage("{}")
	}
	var symbol, agent sql.NullString
	if e.Symbol != "" {
		symbol = sql.NullString{String: e.Symbol, Valid: true}
	}
	if e.Agent != "" {
		agent = sql.NullString{String: e.Agent, Valid: true}
	}
	var outcome sql.NullString
	if len(e.Outcome) > 0 {
		outcome = sql.NullString{String: string(e.Outcome), Valid: true}
	}
	res, err := j.db.ExecContext(ctx,
		`INSERT INTO events (ts, kind, symbol, agent, payload, outcome) VALUES (?, ?, ?, ?, ?, ?)`,
		e.Timestamp.UTC(), e.Kind, symbol, agent, string(e.Payload), outcome,
	)
	if err != nil {
		return 0, fmt.Errorf("journal: insert: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("journal: last id: %w", err)
	}
	return id, nil
}

func (j *Journal) SetOutcome(ctx context.Context, id int64, outcome json.RawMessage) error {
	var oc sql.NullString
	if len(outcome) > 0 {
		oc = sql.NullString{String: string(outcome), Valid: true}
	}
	res, err := j.db.ExecContext(ctx, `UPDATE events SET outcome = ? WHERE id = ?`, oc, id)
	if err != nil {
		return fmt.Errorf("journal: set outcome: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("journal: no event with id %d", id)
	}
	return nil
}

func (j *Journal) Recent(ctx context.Context, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := j.db.QueryContext(ctx,
		`SELECT id, ts, kind, symbol, agent, payload, outcome FROM events ORDER BY id DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("journal: recent: %w", err)
	}
	defer rows.Close()
	return scanEvents(rows)
}

func (j *Journal) BySymbol(ctx context.Context, symbol string, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := j.db.QueryContext(ctx,
		`SELECT id, ts, kind, symbol, agent, payload, outcome FROM events WHERE symbol = ? ORDER BY id DESC LIMIT ?`,
		symbol, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("journal: by symbol: %w", err)
	}
	defer rows.Close()
	return scanEvents(rows)
}

func (j *Journal) Since(ctx context.Context, since time.Time) ([]Event, error) {
	rows, err := j.db.QueryContext(ctx,
		`SELECT id, ts, kind, symbol, agent, payload, outcome FROM events WHERE ts >= ? ORDER BY id ASC`,
		since.UTC(),
	)
	if err != nil {
		return nil, fmt.Errorf("journal: since: %w", err)
	}
	defer rows.Close()
	return scanEvents(rows)
}

func scanEvents(rows *sql.Rows) ([]Event, error) {
	var out []Event
	for rows.Next() {
		var (
			e               Event
			symbol, agent   sql.NullString
			payload         string
			outcome         sql.NullString
		)
		if err := rows.Scan(&e.ID, &e.Timestamp, &e.Kind, &symbol, &agent, &payload, &outcome); err != nil {
			return nil, fmt.Errorf("journal: scan: %w", err)
		}
		if symbol.Valid {
			e.Symbol = symbol.String
		}
		if agent.Valid {
			e.Agent = agent.String
		}
		e.Payload = json.RawMessage(payload)
		if outcome.Valid {
			e.Outcome = json.RawMessage(outcome.String)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
