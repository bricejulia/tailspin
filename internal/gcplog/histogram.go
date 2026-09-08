package gcplog

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/logging"
	"cloud.google.com/go/logging/logadmin"
	"golang.org/x/sync/errgroup"
	"google.golang.org/api/iterator"
)

// SeverityTier groups Cloud Logging's fine-grained Severity levels into the
// small, fixed set of bands tailspin colors by everywhere severity is shown
// (list rows, the detail view, and the histogram) — one classification
// shared by all three so they can never drift apart.
type SeverityTier int

const (
	SeverityTierDefault SeverityTier = iota // Default, Debug
	SeverityTierInfo                        // Info, Notice
	SeverityTierWarning                     // Warning
	SeverityTierError                       // Error, Critical
	SeverityTierAlert                       // Alert, Emergency
	NumSeverityTiers
)

// ClassifySeverity maps a Cloud Logging severity level to its SeverityTier.
func ClassifySeverity(s Severity) SeverityTier {
	switch {
	case s >= logging.Alert:
		return SeverityTierAlert
	case s >= logging.Error:
		return SeverityTierError
	case s >= logging.Warning:
		return SeverityTierWarning
	case s >= logging.Info:
		return SeverityTierInfo
	default:
		return SeverityTierDefault
	}
}

// HistogramBucket is one time bucket's entry counts, broken down by
// SeverityTier.
type HistogramBucket struct {
	Start, End time.Time
	Counts     [NumSeverityTiers]int64
	// Capped reports whether Counts is known to be incomplete: either this
	// bucket hit histogramMaxEntriesPerBucket, or its query failed partway
	// through (most often a transient per-project rate-limit/quota error)
	// after tallying only some of its entries. Either way, Counts is a
	// lower bound, not exact.
	Capped bool
}

// Total sums Counts across every severity tier.
func (b HistogramBucket) Total() int64 {
	var total int64
	for _, c := range b.Counts {
		total += c
	}
	return total
}

// HistogramResult is the ordered set of buckets spanning a query's
// [Since, Until) range, oldest first.
type HistogramResult struct {
	Buckets []HistogramBucket
}

const (
	// histogramBucketConcurrency bounds how many per-bucket queries are in
	// flight at once. It doesn't carry the read-quota-safety burden at all
	// — the gRPC interceptor NewClient installs on c.admin's connection
	// (see ratelimit.go) is what actually keeps total request *issuance*
	// under a project's read quota, regardless of this value — so this is
	// purely about overlapping each bucket's RPC round-trip latency; it
	// does not affect how many requests get made per minute.
	histogramBucketConcurrency = 4

	// histogramMaxEntriesPerBucket caps how many entries a single bucket's
	// tally will page through. Past this, the bucket's Counts become a
	// lower bound (Capped is set) rather than paging a pathologically
	// high-volume bucket to completion. This mainly bounds worst-case
	// *latency* now (client.readLimiter bounds request rate): a bucket
	// that needs many pages, paced at readRequestsPerMinute, can otherwise
	// take a very long time to finish.
	histogramMaxEntriesPerBucket = 5000

	// histogramPageSize is the page size used while paging a bucket's
	// entries. Large, since only Severity is read from each entry here
	// (unlike ListEntries, the rest of the entry — payload, labels — is
	// never materialized) so fewer round trips per bucket matters more than
	// per-page memory.
	histogramPageSize int32 = 1000

	// MaxHistogramBuckets caps how many time buckets Histogram will ever be
	// asked to compute, regardless of how many the caller requests (e.g. one
	// per terminal column on a wide screen) — the other lever, alongside
	// histogramMaxEntriesPerBucket, on total request volume. Exported so
	// tui can request no more than this and skip a wasted refetch when a
	// resize doesn't actually change the (capped) bucket count.
	MaxHistogramBuckets = 60
)

// Histogram computes log-entry counts for f, bucketed into n equal-width
// time buckets spanning [f.Since, f.Until) and broken down by SeverityTier.
// f.Since and f.Until must both already be resolved (non-zero) — the same
// invariant FilterState.Since documents for ListEntries applies here
// identically.
//
// Cloud Logging's v2 API has no native aggregate/count RPC: the vendored
// client library's ListLogEntriesResponse carries only entries and a page
// token, nothing else. So this issues one bounded-concurrency, paginated
// query per bucket, paced by c.readLimiter, and tallies Severity off each
// entry as it streams by, discarding the rest of the entry immediately
// rather than materializing it the way ListEntries does.
//
// A bucket that fails partway through — most often ctx expiring while
// still rate-limit-paced behind other buckets, occasionally a genuine API
// error — never fails the whole call: its Counts are kept as a lower bound
// and HistogramBucket.Capped is set, and every other bucket's result still
// renders. Histogram itself only returns an error for the upfront
// validation above; a caller that wants "did this complete fully" should
// check whether any bucket came back Capped.
func (c *client) Histogram(ctx context.Context, f FilterState, n int) (HistogramResult, error) {
	if f.Since.IsZero() || f.Until.IsZero() {
		return HistogramResult{}, fmt.Errorf("histogram: Since and Until must both be resolved (got Since=%v, Until=%v)", f.Since, f.Until)
	}
	if n <= 0 {
		return HistogramResult{}, fmt.Errorf("histogram: n must be positive, got %d", n)
	}
	// Enforced here too, not just left to the caller: n directly drives
	// request volume (see MaxHistogramBuckets' doc comment), so this is the
	// one place that actually has to hold regardless of what any caller
	// passes.
	n = min(n, MaxHistogramBuckets)

	buckets := bucketBounds(f.Since, f.Until, n)

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(histogramBucketConcurrency)
	for i := range buckets {
		g.Go(func() error {
			counts, capped, err := c.tallyBucket(gctx, f, buckets[i].Start, buckets[i].End)
			buckets[i].Counts = counts
			// A failure (ctx expiring under readLimiter's pacing before
			// this bucket got its turn, or a real API error) leaves
			// Counts a lower bound exactly like hitting
			// histogramMaxEntriesPerBucket does — both are folded into
			// the same Capped signal rather than distinguished, since
			// either way "this bucket's count isn't the full/exact
			// answer" is the only thing a caller needs to know.
			buckets[i].Capped = capped || err != nil
			return nil // never fails the group — see the doc comment above
		})
	}
	_ = g.Wait() // no goroutine above ever returns a non-nil error
	return HistogramResult{Buckets: buckets}, nil
}

// bucketBounds divides [since, until) into n equal-width buckets, oldest
// first. The final bucket's End is pinned to until exactly, rather than
// since+width*n, so integer-duration rounding can never leave a gap after
// the last instant in range.
func bucketBounds(since, until time.Time, n int) []HistogramBucket {
	buckets := make([]HistogramBucket, n)
	width := until.Sub(since) / time.Duration(n)
	for i := range buckets {
		start := since.Add(width * time.Duration(i))
		end := since.Add(width * time.Duration(i+1))
		if i == n-1 {
			end = until
		}
		buckets[i] = HistogramBucket{Start: start, End: end}
	}
	return buckets
}

// tallyBucket pages through every entry matching f narrowed to [start, end),
// tallying counts by SeverityTier, up to histogramMaxEntriesPerBucket.
// Every underlying RPC this issues — including any the SDK's Pager
// internally fires beyond the first to assemble one logical page (see
// ratelimit.go's rateLimitInterceptor doc comment) — is paced by the gRPC
// interceptor NewClient installs on c.admin's connection, not by an
// explicit Wait call here.
func (c *client) tallyBucket(ctx context.Context, f FilterState, start, end time.Time) (counts [NumSeverityTiers]int64, capped bool, err error) {
	bf := f
	bf.Since, bf.Until = start, end

	it := c.admin.Entries(ctx, logadmin.Filter(bf.Build()))
	pager := iterator.NewPager(it, int(histogramPageSize), "")

	var total int64
	for {
		var page []*logging.Entry
		nextToken, err := pager.NextPage(&page)
		if err != nil {
			// Most commonly ctx expiring (histogramTimeout) while this
			// bucket's RPCs were still queued behind the interceptor's
			// pacing, not a genuine API error — folded into Capped by the
			// caller either way (see Histogram's doc comment).
			return counts, false, fmt.Errorf("listing log entries for project %q: %w", c.project, err)
		}
		for _, e := range page {
			counts[ClassifySeverity(e.Severity)]++
			total++
			if total >= histogramMaxEntriesPerBucket {
				return counts, true, nil
			}
		}
		if nextToken == "" {
			return counts, false, nil
		}
	}
}
