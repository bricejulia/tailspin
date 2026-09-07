package gcplog

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/logging"
)

// FilterState is the TUI's filter/query state, translated into a Cloud
// Logging filter string (the same language `gcloud logging read` uses) by
// Build. It has no dependency on I/O and is fully unit-testable on its own.
type FilterState struct {
	MinSeverity Severity // zero value (logging.Default) means "no threshold"

	// ExactSeverity, when set (non-Default), matches exactly that severity
	// level rather than MinSeverity's threshold — set by the facet panel's
	// severity click-to-filter. Wins over MinSeverity when both are set,
	// the same "one field shadows the other" precedent RawQuery sets over
	// the structured fields below.
	ExactSeverity Severity

	LogName      string
	ResourceType string
	FreeText     string

	// Labels holds exact key=value clauses, ANDed together (and with
	// everything else) — set by the facet panel's label click-to-filter.
	// No UI offers anything but equality here.
	Labels map[string]string

	// RawQuery, when non-empty, is used verbatim as the query body
	// instead of the four fields above (mutually exclusive, not
	// combined — see Query). It may itself be a multi-line, multi-clause
	// Cloud Logging filter expression (e.g. pasted from Cloud Console's
	// query builder); the filter language treats newline-separated
	// clauses as implicitly ANDed, same as explicit "AND".
	//
	// The Since/Until invariant below applies to RawQuery exactly as it
	// does to the structured fields: a raw query with no timestamp
	// clause of its own must not be allowed to fall through to
	// logadmin's non-deterministic default either.
	RawQuery string

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

// Query renders the reusable, non-time-bound part of f: RawQuery
// (trimmed) if set, otherwise the same structured-field clauses Build
// uses. Since/Until are deliberately excluded — this is what gets saved
// as a favorite, and a saved favorite should apply relative to "now" when
// reloaded, not to a frozen absolute timestamp from when it was saved.
func (f FilterState) Query() string {
	if raw := strings.TrimSpace(f.RawQuery); raw != "" {
		return raw
	}

	var clauses []string
	switch {
	case f.ExactSeverity > logging.Default:
		clauses = append(clauses, "severity="+strings.ToUpper(f.ExactSeverity.String()))
	case f.MinSeverity > logging.Default:
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
	if len(f.Labels) > 0 {
		keys := make([]string, 0, len(f.Labels))
		for k := range f.Labels {
			keys = append(keys, k)
		}
		sort.Strings(keys) // deterministic Build() output
		for _, k := range keys {
			clauses = append(clauses, fmt.Sprintf("labels.%s=%s", quote(k), quote(f.Labels[k])))
		}
	}
	return strings.Join(clauses, " AND ")
}

// Build renders f as a full Cloud Logging filter expression: Query()
// plus the Since/Until time bound, always ANDed on regardless of whether
// Query() came from RawQuery or the structured fields (see the Since doc
// comment above for why that's non-negotiable).
func (f FilterState) Build() string {
	var clauses []string

	if q := f.Query(); q != "" {
		if strings.TrimSpace(f.RawQuery) != "" {
			// Parenthesized only in the raw case: a pasted query may
			// contain a top-level OR, and ANDing the time bound onto
			// it unparenthesized would silently change precedence.
			// The structured-fields case is already an explicit
			// AND-chain, so no parens there — keeps Build()'s output
			// for that case unchanged.
			clauses = append(clauses, "("+q+")")
		} else {
			clauses = append(clauses, q)
		}
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
