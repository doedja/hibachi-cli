// Package memory is a tiny markdown-file store that the AI reads and writes.
// Files live flat in a single directory. No subdirs, no path traversal.
package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Store struct {
	Dir string
}

func Open(dir string) (*Store, error) {
	if dir == "" {
		return nil, fmt.Errorf("memory: empty dir")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("memory: mkdir %s: %w", dir, err)
	}
	return &Store{Dir: dir}, nil
}

func (s *Store) List() ([]string, error) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return nil, fmt.Errorf("memory: read dir: %w", err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

func (s *Store) Read(name string) (string, error) {
	safe, err := normalizeName(name)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(s.Dir, safe))
	if err != nil {
		return "", fmt.Errorf("memory: read %s: %w", safe, err)
	}
	return string(data), nil
}

func (s *Store) Write(name, content string) error {
	safe, err := normalizeName(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return fmt.Errorf("memory: mkdir: %w", err)
	}
	path := filepath.Join(s.Dir, safe)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("memory: write %s: %w", safe, err)
	}
	return nil
}

func (s *Store) Delete(name string) error {
	safe, err := normalizeName(name)
	if err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(s.Dir, safe)); err != nil {
		return fmt.Errorf("memory: delete %s: %w", safe, err)
	}
	return nil
}

func (s *Store) ReadAll() (string, error) {
	names, err := s.List()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for i, name := range names {
		body, err := s.Read(name)
		if err != nil {
			return "", err
		}
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "--- file: %s ---\n", name)
		b.WriteString(body)
		if !strings.HasSuffix(body, "\n") {
			b.WriteString("\n")
		}
	}
	return b.String(), nil
}

func (s *Store) EstimateTokens() (int, error) {
	names, err := s.List()
	if err != nil {
		return 0, err
	}
	total := 0
	for _, name := range names {
		fi, err := os.Stat(filepath.Join(s.Dir, name))
		if err != nil {
			return 0, fmt.Errorf("memory: stat %s: %w", name, err)
		}
		total += int(fi.Size())
	}
	return total / 4, nil
}

// normalizeName rejects path separators and ".." segments, and forces .md.
// A leading "." (e.g. user passes ".md") is rewritten so the file is not hidden.
func normalizeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("memory: empty name")
	}
	if strings.ContainsAny(name, "/\\") {
		return "", fmt.Errorf("memory: name %q must not contain path separators", name)
	}
	if strings.Contains(name, "..") {
		return "", fmt.Errorf("memory: name %q must not contain ..", name)
	}
	if strings.HasPrefix(name, ".") {
		name = "_" + name
	}
	if !strings.HasSuffix(name, ".md") {
		name += ".md"
	}
	return name, nil
}
