package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// SavedQuery is one favorite: a reusable Cloud Logging query (see
// gcplog.FilterState.Query — the non-time-bound part) under a name, kept
// in tailspin's own config file. Description is never set by tailspin
// itself; it exists purely so a hand-edited or pre-populated entry can
// carry a human-readable label.
type SavedQuery struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Filter      string `json:"filter"`
}

// QueriesPath resolves the path to tailspin's saved-queries file:
// $XDG_CONFIG_HOME/tailspin/queries.json, falling back to
// ~/.config/tailspin/queries.json. This mirrors gcloudDefaultProject's own
// env-var-then-home convention rather than os.UserConfigDir(), which would
// diverge to ~/Library/Application Support on macOS — an XDG-style path is
// what most CLI tools' config files already look like, and is what a user
// hand-editing this file would expect to find.
func QueriesPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locating config dir: %w", err)
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "tailspin", "queries.json"), nil
}

// LoadQueries reads the saved queries at path. A missing file returns
// (nil, nil) — not an error, since a user who's never run :save yet
// shouldn't see one. Malformed JSON is reported as an error (the path is
// included) so a bad hand edit is visible rather than silently discarding
// every saved query.
func LoadQueries(path string) ([]SavedQuery, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var queries []SavedQuery
	if err := json.Unmarshal(b, &queries); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return queries, nil
}

// SaveQuery upserts q by exact-match Name into the file at path, creating
// the parent directory and file if needed. It reads the current contents
// first (tolerating a missing file like LoadQueries, but propagating a
// malformed-file error rather than silently clobbering an unparseable
// hand edit), then writes the result back via a temp-file-plus-rename so a
// crash mid-write can't corrupt the file.
func SaveQuery(path string, q SavedQuery) error {
	queries, err := LoadQueries(path)
	if err != nil {
		return err
	}

	replaced := false
	for i, existing := range queries {
		if existing.Name == q.Name {
			queries[i] = q
			replaced = true
			break
		}
	}
	if !replaced {
		queries = append(queries, q)
	}

	b, err := json.MarshalIndent(queries, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".queries-*.json.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }() // no-op once the rename below succeeds

	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing %s: %w", tmpPath, err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return fmt.Errorf("setting permissions on %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("saving %s: %w", path, err)
	}
	return nil
}

// FindQuery returns the entry named name (exact match) from queries, and
// whether one was found.
func FindQuery(queries []SavedQuery, name string) (SavedQuery, bool) {
	for _, q := range queries {
		if q.Name == name {
			return q, true
		}
	}
	return SavedQuery{}, false
}
