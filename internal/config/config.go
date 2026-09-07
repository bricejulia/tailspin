// Package config resolves tailspin's runtime configuration: which GCP
// project to point at, following the same conventions as the gcloud CLI.
package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config is tailspin's resolved runtime configuration.
type Config struct {
	Project string
}

// DefaultReadQuota is Cloud Logging's own default "read requests per
// minute" quota per project (see cloud.google.com/logging/quotas).
// tailspin paces its own read requests — ListEntries pagination, and the
// histogram's per-bucket queries — to this budget by default, so ordinary
// use doesn't trip it. See ResolveReadQuota to override it, e.g. for a
// project with a raised quota, or one shared with other tools already
// consuming part of it.
const DefaultReadQuota = 60

// ResolveReadQuota picks the read-requests-per-minute budget tailspin
// paces itself to, checking sources in priority order: an explicit flag
// value, then $TAILSPIN_READ_QUOTA, then DefaultReadQuota.
func ResolveReadQuota(flagVal int) (int, error) {
	if flagVal > 0 {
		return flagVal, nil
	}
	if v := os.Getenv("TAILSPIN_READ_QUOTA"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("invalid $TAILSPIN_READ_QUOTA %q: must be a positive integer", v)
		}
		return n, nil
	}
	return DefaultReadQuota, nil
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
