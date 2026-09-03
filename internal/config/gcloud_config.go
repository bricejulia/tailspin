package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// gcloudDefaultProject reads the active gcloud CLI configuration and returns
// the project it has set, without shelling out to gcloud itself.
//
// gcloud stores its config under $CLOUDSDK_CONFIG (or ~/.config/gcloud on
// most platforms): a file named "active_config" names the active
// configuration (defaulting to "default"), and
// "configurations/config_<name>" is a plain INI file with a "project" key
// under the [core] section.
//
// Note: user credentials from `gcloud auth application-default login` are
// "authorized_user" credentials and do not embed a project ID, so this file
// is the only reliable local source for gcloud's notion of the default
// project.
func gcloudDefaultProject() (string, error) {
	dir := os.Getenv("CLOUDSDK_CONFIG")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locating gcloud config dir: %w", err)
		}
		dir = filepath.Join(home, ".config", "gcloud")
	}

	activeConfig := "default"
	if b, err := os.ReadFile(filepath.Join(dir, "active_config")); err == nil {
		if name := strings.TrimSpace(string(b)); name != "" {
			activeConfig = name
		}
	}

	configPath := filepath.Join(dir, "configurations", "config_"+activeConfig)
	f, err := os.Open(configPath)
	if err != nil {
		return "", fmt.Errorf("reading gcloud config %s: %w", configPath, err)
	}
	defer f.Close()

	inCore := true
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inCore = line == "[core]"
			continue
		}
		if !inCore {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "project" {
			continue
		}
		if project := strings.TrimSpace(val); project != "" {
			return project, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("parsing gcloud config %s: %w", configPath, err)
	}

	return "", fmt.Errorf("no project set in gcloud config %s", configPath)
}
