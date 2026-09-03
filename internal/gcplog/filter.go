package gcplog

import (
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/logging"
)

// FilterState is the TUI's filter/query state, translated into a Cloud
// Logging filter string (the same language `gcloud logging read` uses) by
// Build. It has no dependency on I/O and is fully unit-testable on its own.
type FilterState struct {
	MinSeverity  Severity // zero value (logging.Default) means "no threshold"
	LogName      string
	ResourceType string
	FreeText     string

	// Since and Until bound the query's time range. Zero means unbounded.
	//
	// Gotcha: if a FilterState with a zero Since builds a filter string
	// with no "timestamp" clause at all, logadmin.Client.Entries silently
	// ANDs in its own `timestamp >= now-24h` default — computed fresh
	// from time.Now() on every call. Since ListEntries is called anew
	// per page (by design: no long-lived iterator sitting between UI
	// ticks), that makes the effective filter string drift between
	// pages of what's supposed to be one paginated query, which the
	// Cloud Logging API rejects ("page_token doesn't match arguments
	// from which it was generated"). Callers that want browse mode's
	// default lookback window must resolve a concrete Since once, up
	// front, and reuse that same FilterState value across every page of
	// a query (see tui.New's defaultLookback) rather than leaving it
	// zero and relying on logadmin's implicit, non-deterministic default.
	Since time.Time
	Until time.Time
}

// Build renders f as a Cloud Logging filter expression.
func (f FilterState) Build() string {
	var clauses []string

	if f.MinSeverity > logging.Default {
		// logging.Severity.String() renders "Warning", "Error", etc.
		// (title case); the filter language's canonical enum spelling
		// is uppercase ("WARNING", "ERROR").
		clauses = append(clauses, "severity>="+strings.ToUpper(f.MinSeverity.String()))
	}
	if f.LogName != "" {
		clauses = append(clauses, fmt.Sprintf("logName:%s", quote(f.LogName)))
	}
	if f.ResourceType != "" {
		clauses = append(clauses, fmt.Sprintf("resource.type=%s", quote(f.ResourceType)))
	}
	if f.FreeText != "" {
		// jsonPayload is a nested/structured field: a bare `jsonPayload:"x"`
		// comparison is rejected by the API ("Cannot match a nested type").
		// SEARCH() is the Cloud Logging filter language's indexed
		// free-text function and correctly covers textPayload,
		// jsonPayload's string fields, and labels in one go.
		clauses = append(clauses, fmt.Sprintf("SEARCH(%s)", quote(f.FreeText)))
	}
	if !f.Since.IsZero() {
		clauses = append(clauses, fmt.Sprintf("timestamp>=%s", quote(f.Since.UTC().Format(time.RFC3339))))
	}
	if !f.Until.IsZero() {
		clauses = append(clauses, fmt.Sprintf("timestamp<=%s", quote(f.Until.UTC().Format(time.RFC3339))))
	}

	return strings.Join(clauses, " AND ")
}

// quote renders s as a double-quoted Cloud Logging filter string literal,
// escaping embedded quotes and backslashes.
func quote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
