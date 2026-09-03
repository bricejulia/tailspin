package config

import (
	"os"
	"path/filepath"
	"testing"
)

// withGcloudConfig points CLOUDSDK_CONFIG at a scratch directory laid out
// like a real gcloud config dir, with the given active configuration's
// [core] project set. No real gcloud installation or network access needed.
func withGcloudConfig(t *testing.T, project string) {
	t.Helper()
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "active_config"), []byte("default\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	configsDir := filepath.Join(dir, "configurations")
	if err := os.MkdirAll(configsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	content := "[core]\naccount = someone@example.com\n"
	if project != "" {
		content += "project = " + project + "\n"
	}
	if err := os.WriteFile(filepath.Join(configsDir, "config_default"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CLOUDSDK_CONFIG", dir)
}

func TestResolveProject_FlagWins(t *testing.T) {
	withGcloudConfig(t, "gcloud-project")
	t.Setenv("GOOGLE_CLOUD_PROJECT", "env-project")

	got, err := ResolveProject("flag-project")
	if err != nil {
		t.Fatal(err)
	}
	if got != "flag-project" {
		t.Errorf("ResolveProject = %q, want %q", got, "flag-project")
	}
}

func TestResolveProject_EnvOverGcloudConfig(t *testing.T) {
	withGcloudConfig(t, "gcloud-project")
	t.Setenv("GOOGLE_CLOUD_PROJECT", "env-project")

	got, err := ResolveProject("")
	if err != nil {
		t.Fatal(err)
	}
	if got != "env-project" {
		t.Errorf("ResolveProject = %q, want %q", got, "env-project")
	}
}

func TestResolveProject_TailspinEnvVar(t *testing.T) {
	withGcloudConfig(t, "gcloud-project")
	t.Setenv("TAILSPIN_PROJECT", "tailspin-project")

	got, err := ResolveProject("")
	if err != nil {
		t.Fatal(err)
	}
	if got != "tailspin-project" {
		t.Errorf("ResolveProject = %q, want %q", got, "tailspin-project")
	}
}

func TestResolveProject_FallsBackToGcloudConfig(t *testing.T) {
	withGcloudConfig(t, "gcloud-project")

	got, err := ResolveProject("")
	if err != nil {
		t.Fatal(err)
	}
	if got != "gcloud-project" {
		t.Errorf("ResolveProject = %q, want %q", got, "gcloud-project")
	}
}

func TestResolveProject_NoneConfigured(t *testing.T) {
	withGcloudConfig(t, "") // no project line at all

	_, err := ResolveProject("")
	if err == nil {
		t.Fatal("ResolveProject returned nil error, want an error")
	}
}
