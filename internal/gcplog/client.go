// Package gcplog is the seam between tailspin's TUI and GCP: it talks to
// Cloud Logging directly via the Go client libraries (never by shelling out
// to gcloud) and exposes a small, TUI-agnostic Client interface that can be
// faked in tests without any network access.
package gcplog

import (
	"context"
	"fmt"
	"time"

	apiv2 "cloud.google.com/go/logging/apiv2"
	"cloud.google.com/go/logging/logadmin"
)

// readLimiterWindow is the trailing window client's readLimiter enforces
// "at most readRequestsPerMinute requests" over — 60s, matching Cloud
// Logging's own quota being stated "per minute". A var, not a const, so a
// future test can shrink it, the same "production duration, test-shrinks-
// it" idiom internal/tui/app.go's histogramResizeDebounce already uses —
// though slidingWindowLimiter's own constructor also takes a window
// directly, so ratelimit_test.go doesn't actually need to touch this var
// to exercise a short window.
var readLimiterWindow = 60 * time.Second

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

	// Read-request pacing lives entirely in the gRPC interceptor NewClient
	// installs on admin's connection (see ratelimit.go and
	// newReadLimiterDialOption) — every read RPC this Client issues
	// (ListEntries pagination, Histogram/Facets' per-bucket/per-range
	// queries, including any extra RPC the SDK's Pager fires internally
	// beyond the first to assemble one logical page) is paced there, to at
	// most readRequestsPerMinute per readLimiterWindow, so tailspin stays
	// under Cloud Logging's own read quota instead of bursting until the
	// API starts rejecting requests. Nothing here holds a reference to the
	// limiter itself — the interceptor closure already captures it, and
	// nothing in this package needs to reach it again after construction.
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
	// Installed once, here, at Dial time — not as a Wait() call sprinkled
	// at each read call site — so it gates every real unary RPC admin
	// issues, including ones a single Pager.NextPage() call triggers
	// internally when assembling one logical page needs more than one
	// server response (see ratelimit.go's rateLimitInterceptor doc
	// comment). A quiet client still bursts through up to
	// readRequestsPerMinute requests instantly (see slidingWindowLimiter's
	// doc comment) — a typical histogram/facets query still drains most
	// of a quiet budget in one quick burst, the same responsiveness the
	// old token-bucket design wanted — but never more than that within
	// any readLimiterWindow, unlike the old design's burst-on-top-of-
	// refill overshoot.
	_, readLimiterOpt := newReadLimiterDialOption(readRequestsPerMinute, readLimiterWindow)
	admin, err := logadmin.NewClient(ctx, project, readLimiterOpt)
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
	}, nil
}

func (c *client) Close() error {
	err := c.admin.Close()
	if streamErr := c.stream.Close(); err == nil {
		err = streamErr
	}
	return err
}
