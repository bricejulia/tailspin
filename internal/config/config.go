// Package config resolves tailspin's runtime configuration: which GCP
// project to point at, following the same conventions as the gcloud CLI.
package config

import (
	"fmt"
	"os"
)

// Config is tailspin's resolved runtime configuration.
type Config struct {
	Project string
}

// ResolveProject picks the GCP project tailspin should talk to, checking
// sources in priority order: an explicit flag value, then the
// GOOGLE_CLOUD_PROJECT and TAILSPIN_PROJECT environment variables, then
// gcloud's own active configuration file. It never shells out to gcloud.
func ResolveProject(flagVal string) (string, error) {
	if flagVal != "" {
		return flagVal, nil
	}
	for _, envVar := range []string{"GOOGLE_CLOUD_PROJECT", "TAILSPIN_PROJECT"} {
		if v := os.Getenv(envVar); v != "" {
			return v, nil
		}
	}
	if project, err := gcloudDefaultProject(); err == nil {
		return project, nil
	}
	return "", fmt.Errorf("no GCP project configured: pass --project, set $GOOGLE_CLOUD_PROJECT or $TAILSPIN_PROJECT, or run `gcloud config set project <id>`")
}
