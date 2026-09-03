package gcplog

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/logging"
	"cloud.google.com/go/logging/apiv2/loggingpb"
)

// maxSummaryLen bounds how much of a payload's rendering is kept for the
// list row's Summary. It's a safety cap against a pathological single
// entry (thousands of these are held in memory at once for a loaded page),
// not a display width — the list view itself now offers wrap and
// horizontal-scroll to read a long entry in full, so this needs to be
// generous enough that hitting the cap is rare for real payloads, not
// tuned to "one screen line".
const maxSummaryLen = 4096

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

// logNameEscaper undoes the URL-encoding Cloud Logging applies to log IDs
// within a log resource name (e.g. "...%2Factivity" -> ".../activity").
var logNameEscaper = strings.NewReplacer("%2F", "/", "%2f", "/")

// entryFromProto converts a raw streamed LogEntry (from TailLogEntries) into
// an Entry. logadmin.Entries does the equivalent conversion internally for
// historical queries, but doesn't export it, so tailing builds its own —
// same field mapping, only the payload oneof and severity enum need
// unwrapping.
func entryFromProto(le *loggingpb.LogEntry) (Entry, error) {
	if err := le.GetTimestamp().CheckValid(); err != nil {
		return Entry{}, fmt.Errorf("invalid timestamp: %w", err)
	}

	var payload any
	switch p := le.GetPayload().(type) {
	case *loggingpb.LogEntry_TextPayload:
		payload = p.TextPayload
	case *loggingpb.LogEntry_JsonPayload:
		payload = p.JsonPayload.AsMap()
	case *loggingpb.LogEntry_ProtoPayload:
		msg, err := p.ProtoPayload.UnmarshalNew()
		if err != nil {
			return Entry{}, fmt.Errorf("unmarshalling proto payload: %w", err)
		}
		payload = msg
	}

	entry := Entry{
		Timestamp: le.GetTimestamp().AsTime(),
		Severity:  Severity(le.GetSeverity()),
		LogName:   logNameEscaper.Replace(le.GetLogName()),
		Labels:    le.GetLabels(),
		Payload:   payload,
	}
	if r := le.GetResource(); r != nil {
		entry.Resource = r.Type
	}
	entry.Summary = summarize(entry.Payload)
	return entry, nil
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
