package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQueriesPath_XDGOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	got, err := QueriesPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "tailspin", "queries.json")
	if got != want {
		t.Errorf("QueriesPath() = %q, want %q", got, want)
	}
}

func TestQueriesPath_FallsBackToHomeConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")

	got, err := QueriesPath()
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".config", "tailspin", "queries.json")
	if got != want {
		t.Errorf("QueriesPath() = %q, want %q", got, want)
	}
}

func TestLoadQueries_MissingFileReturnsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist", "queries.json")

	queries, err := LoadQueries(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if queries != nil {
		t.Errorf("queries = %v, want nil", queries)
	}
}

func TestLoadQueries_Malformed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queries.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadQueries(path)
	if err == nil {
		t.Fatal("LoadQueries returned nil error for malformed JSON, want an error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not mention the file path %q", err.Error(), path)
	}
}

func TestSaveQuery_CreatesDirAndFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "queries.json")

	q := SavedQuery{Name: "k8s", Filter: `resource.type="k8s_container"`}
	if err := SaveQuery(path, q); err != nil {
		t.Fatal(err)
	}

	got, err := LoadQueries(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != q {
		t.Errorf("LoadQueries() = %+v, want [%+v]", got, q)
	}
}

func TestSaveQuery_UpsertsByName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queries.json")

	other := SavedQuery{Name: "other", Filter: "severity>=ERROR"}
	if err := SaveQuery(path, other); err != nil {
		t.Fatal(err)
	}
	original := SavedQuery{Name: "k8s", Filter: `resource.type="k8s_container"`}
	if err := SaveQuery(path, original); err != nil {
		t.Fatal(err)
	}
	updated := SavedQuery{Name: "k8s", Filter: `resource.type="k8s_pod"`}
	if err := SaveQuery(path, updated); err != nil {
		t.Fatal(err)
	}

	got, err := LoadQueries(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("LoadQueries() returned %d entries, want 2: %+v", len(got), got)
	}
	gotOther, ok := FindQuery(got, "other")
	if !ok || gotOther != other {
		t.Errorf("unrelated entry %q = %+v, want unchanged %+v", "other", gotOther, other)
	}
	gotK8s, ok := FindQuery(got, "k8s")
	if !ok || gotK8s != updated {
		t.Errorf("upserted entry %q = %+v, want %+v", "k8s", gotK8s, updated)
	}
}

func TestFindQuery(t *testing.T) {
	queries := []SavedQuery{
		{Name: "a", Filter: "one"},
		{Name: "b", Filter: "two"},
	}

	if got, ok := FindQuery(queries, "b"); !ok || got.Filter != "two" {
		t.Errorf("FindQuery(%q) = %+v, %v; want the \"b\" entry, true", "b", got, ok)
	}
	if _, ok := FindQuery(queries, "missing"); ok {
		t.Error("FindQuery(missing) = true, want false")
	}
}
