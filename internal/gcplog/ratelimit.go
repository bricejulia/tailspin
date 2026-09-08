package gcplog

import (
	"context"
	"sync"
	"time"

	"google.golang.org/api/option"
	"google.golang.org/grpc"
)

// slidingWindowLimiter enforces a hard cap: at most limit calls to Wait are
// granted within any trailing window-long interval, measured from each
// individual grant — not divided into fixed-size buckets, so there's no
// edge effect at an arbitrary bucket boundary.
//
// Unlike golang.org/x/time/rate.Limiter's token bucket (this package's
// former choice), this makes no allowance for "saved up" capacity beyond
// limit: the (limit+1)th request in any rolling window always waits,
// however long a backlog is queued. That's the whole point — a token
// bucket's provable bound is "at most burst + rate*window requests in any
// window of length window", which with burst==limit allows up to 2x limit
// in a rolling window whenever there's a queued backlog (exactly what
// tripped Cloud Logging's real read-requests-per-minute quota despite
// tailspin's own limiter believing it was staying under it). This type
// instead guarantees "at most limit, full stop".
//
// A quiet limiter (no grants in the last window) still grants up to limit
// requests instantly, the same as a token bucket's burst — so a typical
// histogram/facets query still bursts through at full speed when the
// client has been idle. The difference only shows up once more than limit
// requests are actually in flight/queued within one window, which is
// exactly the case that used to overshoot.
type slidingWindowLimiter struct {
	limit  int
	window time.Duration

	mu      sync.Mutex
	granted []time.Time // len <= limit; oldest first
}

// newSlidingWindowLimiter returns a limiter granting at most limit calls to
// Wait in any trailing window-length interval. limit and window must be
// positive; NewClient already rejects a non-positive readRequestsPerMinute
// before this is ever constructed.
func newSlidingWindowLimiter(limit int, window time.Duration) *slidingWindowLimiter {
	return &slidingWindowLimiter{
		limit:   limit,
		window:  window,
		granted: make([]time.Time, 0, limit),
	}
}

// Wait blocks until a grant is available under the limit, records it, and
// returns nil — or returns ctx.Err() if ctx is done first, without
// recording a grant, so a caller that gives up while queued doesn't
// consume budget it never used.
func (l *slidingWindowLimiter) Wait(ctx context.Context) error {
	for {
		l.mu.Lock()
		now := time.Now()
		l.evictLocked(now)
		if len(l.granted) < l.limit {
			l.granted = append(l.granted, now)
			l.mu.Unlock()
			return nil
		}
		wait := l.granted[0].Add(l.window).Sub(now)
		l.mu.Unlock()

		if wait <= 0 {
			continue // evictLocked should already have dropped it; guard a zero/negative timer
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
			// Loop back around and re-check under lock: concurrent
			// waiters may race for the slot that just freed up.
		}
	}
}

// evictLocked drops every recorded grant older than window from now.
// l.mu must be held by the caller.
func (l *slidingWindowLimiter) evictLocked(now time.Time) {
	cutoff := now.Add(-l.window)
	i := 0
	for i < len(l.granted) && l.granted[i].Before(cutoff) {
		i++
	}
	if i > 0 {
		l.granted = append(l.granted[:0], l.granted[i:]...)
	}
}

// rateLimitInterceptor returns a grpc.UnaryClientInterceptor that paces
// every unary RPC through limiter before invoking it. Installed once, at
// Dial time, via option.WithGRPCDialOption(grpc.WithChainUnaryInterceptor(...))
// in NewClient (see client.go) — this is what makes pacing apply to every
// real RPC the admin client's Pager issues internally, including the 2nd+
// RPC within a single NextPage() call that has to assemble one logical
// page out of more than one server response (Cloud Logging's
// ListLogEntries responses are capped by response size, not just the
// requested page size, so this is routine at the 1000-entry pages
// Histogram/Facets request) — a gap the old per-NextPage-call Wait() left
// completely unpaced beyond the first RPC.
//
// grpc.WithChainUnaryInterceptor (not WithUnaryInterceptor) is used
// deliberately: the latter would silently overwrite any other interceptor
// the SDK's own client options install, since dial options are applied in
// order and only the last WithUnaryInterceptor "wins".
func rateLimitInterceptor(limiter *slidingWindowLimiter) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if err := limiter.Wait(ctx); err != nil {
			return err
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// newReadLimiterDialOption builds the slidingWindowLimiter that paces read
// RPCs to at most limit per window, and the option.ClientOption that wires
// its interceptor into a logadmin.NewClient call (see NewClient in
// client.go). Split out as a pure function — no network, no credentials —
// specifically so ratelimit_test.go can assert NewClient configures the
// right budget without needing real GCP credentials the way calling
// NewClient itself (and so logadmin.NewClient, which resolves Application
// Default Credentials at Dial time) would. The limiter is returned
// alongside the option purely for that introspection; NewClient itself
// has no further use for it once the option is built — the interceptor
// closure already captures it.
func newReadLimiterDialOption(limit int, window time.Duration) (*slidingWindowLimiter, option.ClientOption) {
	limiter := newSlidingWindowLimiter(limit, window)
	return limiter, option.WithGRPCDialOption(grpc.WithChainUnaryInterceptor(rateLimitInterceptor(limiter)))
}
