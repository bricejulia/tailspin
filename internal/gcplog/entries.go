package gcplog

import (
	"encoding/json"
	"strings"
	"time"

	"cloud.google.com/go/logging"
)

// maxSummaryLen bounds how much of a payload's rendering shows up as the
// one-line list-row summary.
const maxSummaryLen = 200

// Severity is a Cloud Logging severity level (DEFAULT, DEBUG, INFO, ...).
type Severity = logging.Severity

// Entry is a single log entry, decoupled from the underlying GCP SDK types
// so the tui package never needs to import them directly.
type Entry struct {
	Timestamp time.Time
	Severity  Severity
	LogName   string
	Resource  string
	Summary   string // first line of the payload, precomputed for list rows
	Labels    map[string]string
	Payload   any
}

func entryFromSDK(e *logging.Entry) Entry {
	entry := Entry{
		Timestamp: e.Timestamp,
		Severity:  e.Severity,
		LogName:   e.LogName,
		Labels:    e.Labels,
		Payload:   e.Payload,
	}
	if e.Resource != nil {
		entry.Resource = e.Resource.Type
	}
	entry.Summary = summarize(e.Payload)
	return entry
}

// summarize renders a single-line preview of a log entry's payload for use
// in the list view.
func summarize(payload any) string {
	var s string
	switch p := payload.(type) {
	case string:
		s = p
	case nil:
		s = ""
	default:
		s = trimJSONPreview(p)
	}
	if line, _, found := strings.Cut(s, "\n"); found {
		s = line
	}
	if len(s) > maxSummaryLen {
		s = s[:maxSummaryLen] + "…"
	}
	return s
}

// trimJSONPreview renders a structured payload (jsonPayload / protoPayload)
// as compact single-line JSON for the summary field; the full structure is
// still available via Entry.Payload for the detail view.
func trimJSONPreview(payload any) string {
	b, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(b)
}
