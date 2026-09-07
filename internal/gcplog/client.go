// Package gcplog is the seam between tailspin's TUI and GCP: it talks to
// Cloud Logging directly via the Go client libraries (never by shelling out
// to gcloud) and exposes a small, TUI-agnostic Client interface that can be
// faked in tests without any network access.
package gcplog

import (
	"context"
	"fmt"

	apiv2 "cloud.google.com/go/logging/apiv2"
	"cloud.google.com/go/logging/logadmin"
	"golang.org/x/time/rate"
)

// Page is one page of historical log entries.
type Page struct {
	Entries       []Entry
	NextPageToken string
}

// TailEvent is one message from a live tail stream: either a new Entry, or
// a terminal Err after which the events channel is closed.
type TailEvent struct {
	Entry Entry
	Err   error
}

// Client is everything the TUI needs from GCP Cloud Logging.
type Client interface {
	// ListEntries fetches one page of historical entries matching f,
	// newest first.
	ListEntries(ctx context.Context, f FilterState, pageToken string, pageSize int32) (Page, error)
	// TailEntries starts a live tail stream matching f. The returned
	// channel is closed after a terminal TailEvent.Err (including
	// context cancellation); cancel stops the stream early.
	TailEntries(ctx context.Context, f FilterState) (events <-chan TailEvent, cancel func(), err error)
	// Histogram computes exact log-entry counts for f, bucketed into n
	// equal-width time buckets and broken down by severity band. Cloud
	// Logging's v2 API has no native aggregate/count RPC, so this is built
	// out of n fully-paginated queries (bounded concurrency, capped per
	// bucket) rather than a single call — see the implementation's doc
	// comment for why. f.Since and f.Until must both be resolved.
	Histogram(ctx context.Context, f FilterState, n int) (HistogramResult, error)
	// Facets computes a field-by-field breakdown (Severity, LogName,
	// Resource, Labels) of every entry matching f across its full
	// [Since, Until) range, for the facet side panel. Like Histogram, this
	// is built out of several fully-paginated queries rather than a
	// single call — see the implementation's doc comment. f.Since and
	// f.Until must both be resolved.
	Facets(ctx context.Context, f FilterState) (FacetsResult, error)
	Close() error
}

// client is the real Client implementation, backed by the Cloud Logging
// client libraries: logadmin for paged historical queries, and the
// lower-level generated apiv2 client for TailLogEntries — the streaming
// tail RPC isn't exposed by logadmin or the high-level logging package, and
// requires gRPC transport (which both clients use by default here; neither
// is constructed with any REST-forcing option).
type client struct {
	project string
	admin   *logadmin.Client
	stream  *apiv2.Client

	// readLimiter paces every read RPC this Client issues — ListEntries
	// pagination and Histogram's per-bucket queries — to at most
	// readRequestsPerMinute per minute (see NewClient), so tailspin stays
	// under Cloud Logging's own read quota instead of bursting until the
	// API starts rejecting requests.
	readLimiter *rate.Limiter
}

// NewClient creates a Client for the given GCP project, using Application
// Default Credentials (the same credentials `gcloud auth
// application-default login` sets up). readRequestsPerMinute must be
// positive; see internal/config.ResolveReadQuota for how tailspin resolves
// it (Cloud Logging's own default read quota unless overridden).
func NewClient(ctx context.Context, project string, readRequestsPerMinute int) (Client, error) {
	if readRequestsPerMinute <= 0 {
		return nil, fmt.Errorf("readRequestsPerMinute must be positive, got %d", readRequestsPerMinute)
	}
	admin, err := logadmin.NewClient(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("creating Cloud Logging client for project %q: %w", project, err)
	}
	stream, err := apiv2.NewClient(ctx)
	if err != nil {
		_ = admin.Close()
		return nil, fmt.Errorf("creating Cloud Logging streaming client for project %q: %w", project, err)
	}
	return &client{
		project: project,
		admin:   admin,
		stream:  stream,
		// Burst == readRequestsPerMinute: the standard token-bucket
		// modeling of an "N per minute" quota — a full minute's budget is
		// available immediately after being idle, then sustained usage
		// paces at readRequestsPerMinute/60 per second. This matters in
		// practice: a burst of 1 (tried first) meant every single read
		// request — across ListEntries pagination and every histogram
		// bucket alike — was serialized a full 60/readRequestsPerMinute
		// seconds apart with no exceptions, so even a typical histogram
		// (tens of buckets, each usually needing just one page) took the
		// better part of a minute at the default 60/minute quota — it
		// wasn't hung, just so slow it read as stuck. Burst ==
		// readRequestsPerMinute instead lets a typical histogram drain
		// most or all of a quiet client's budget in one quick burst — the
		// exact usage pattern a "requests per minute" quota is meant to
		// allow — while still never exceeding readRequestsPerMinute
		// sustained requests in any minute.
		readLimiter: rate.NewLimiter(rate.Limit(float64(readRequestsPerMinute)/60), readRequestsPerMinute),
	}, nil
}

func (c *client) Close() error {
	err := c.admin.Close()
	if streamErr := c.stream.Close(); err == nil {
		err = streamErr
	}
	return err
}
