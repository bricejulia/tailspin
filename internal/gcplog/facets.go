package gcplog

import (
	"context"
	"fmt"
	"sort"
	"time"

	"cloud.google.com/go/logging"
	"cloud.google.com/go/logging/logadmin"
	"golang.org/x/sync/errgroup"
	"google.golang.org/api/iterator"
)

// FacetCount is one distinct field value and how many loaded entries carry
// it.
type FacetCount struct {
	Value string
	Count int
}

// SeverityFacetCount is FacetCount's severity-specific counterpart: it
// keeps the actual Severity value (rather than just its rendered string)
// so a caller can turn a selected facet straight back into an exact-match
// filter without lossily parsing a formatted label back into an enum.
type SeverityFacetCount struct {
	Severity Severity
	Count    int
}

// LabelFacet is one distinct label key's value breakdown.
type LabelFacet struct {
	Key    string
	Values []FacetCount
}

// Facets is a field-by-field breakdown of a set of log entries — the
// facet panel's equivalent of the histogram's HistogramResult. Every
// bucket is sorted count descending, with ties broken by value ascending
// (Severity ascending for Severity) so the ordering is deterministic.
type Facets struct {
	Severity []SeverityFacetCount
	LogName  []FacetCount
	Resource []FacetCount
	Labels   []LabelFacet // one per distinct label key seen, keys sorted
}

// FacetAggregator folds entries into running per-field counts without
// retaining the entries themselves, so a caller paging through many
// entries (see client.Facets) can discard each one immediately after
// tallying it rather than holding the whole result set in memory at once.
// The zero value is ready to use.
type FacetAggregator struct {
	severity map[Severity]int
	logName  map[string]int
	resource map[string]int
	labels   map[string]map[string]int
}

// Add folds one entry's Severity, LogName, Resource, and Labels into the
// running counts. LogName/Resource are skipped when empty — an empty
// facet row isn't meaningful to show or click.
func (a *FacetAggregator) Add(e Entry) {
	a.add(e.Severity, e.LogName, e.Resource, e.Labels)
}

// add is Add's field-level counterpart, used directly by client.Facets'
// paging loop so it can tally straight off the SDK's *logging.Entry
// without first materializing a full Entry (Payload/Summary included) for
// fields the facet panel never shows.
func (a *FacetAggregator) add(sev Severity, logName, resource string, labels map[string]string) {
	if a.severity == nil {
		a.severity = map[Severity]int{}
	}
	a.severity[sev]++

	if logName != "" {
		if a.logName == nil {
			a.logName = map[string]int{}
		}
		a.logName[logName]++
	}

	if resource != "" {
		if a.resource == nil {
			a.resource = map[string]int{}
		}
		a.resource[resource]++
	}

	for k, v := range labels {
		if a.labels == nil {
			a.labels = map[string]map[string]int{}
		}
		if a.labels[k] == nil {
			a.labels[k] = map[string]int{}
		}
		a.labels[k][v]++
	}
}

// AddAll folds every entry in es into the running counts.
func (a *FacetAggregator) AddAll(es []Entry) {
	for _, e := range es {
		a.Add(e)
	}
}

// Merge folds other's running counts into a — used to combine the
// independent aggregators built by client.Facets' concurrent per-range
// goroutines into one final result.
func (a *FacetAggregator) Merge(other FacetAggregator) {
	for sev, c := range other.severity {
		if a.severity == nil {
			a.severity = map[Severity]int{}
		}
		a.severity[sev] += c
	}
	for v, c := range other.logName {
		if a.logName == nil {
			a.logName = map[string]int{}
		}
		a.logName[v] += c
	}
	for v, c := range other.resource {
		if a.resource == nil {
			a.resource = map[string]int{}
		}
		a.resource[v] += c
	}
	for k, vals := range other.labels {
		if a.labels == nil {
			a.labels = map[string]map[string]int{}
		}
		if a.labels[k] == nil {
			a.labels[k] = map[string]int{}
		}
		for v, c := range vals {
			a.labels[k][v] += c
		}
	}
}

// Result renders the running counts as a Facets, each bucket sorted count
// descending (value/Severity ascending as the tiebreak).
func (a FacetAggregator) Result() Facets {
	var f Facets

	for sev, c := range a.severity {
		f.Severity = append(f.Severity, SeverityFacetCount{Severity: sev, Count: c})
	}
	sort.Slice(f.Severity, func(i, j int) bool {
		if f.Severity[i].Count != f.Severity[j].Count {
			return f.Severity[i].Count > f.Severity[j].Count
		}
		return f.Severity[i].Severity < f.Severity[j].Severity
	})

	f.LogName = sortedFacetCounts(a.logName)
	f.Resource = sortedFacetCounts(a.resource)

	labelKeys := make([]string, 0, len(a.labels))
	for k := range a.labels {
		labelKeys = append(labelKeys, k)
	}
	sort.Strings(labelKeys)
	for _, k := range labelKeys {
		f.Labels = append(f.Labels, LabelFacet{Key: k, Values: sortedFacetCounts(a.labels[k])})
	}

	return f
}

func sortedFacetCounts(m map[string]int) []FacetCount {
	if len(m) == 0 {
		return nil // keeps an empty field nil, consistent with Severity/Labels above
	}
	out := make([]FacetCount, 0, len(m))
	for v, c := range m {
		out = append(out, FacetCount{Value: v, Count: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Value < out[j].Value
	})
	return out
}

// BuildFacets aggregates entries in one batch call — for tail mode, which
// already holds every entry it'll ever facet in memory (tailModel.entries),
// unlike browse mode's fetch-all (see client.Facets), which folds and
// discards entries page by page instead.
func BuildFacets(entries []Entry) Facets {
	var a FacetAggregator
	a.AddAll(entries)
	return a.Result()
}

// FacetsResult is the outcome of a Client.Facets call.
type FacetsResult struct {
	Facets Facets
	// Capped reports whether Facets is known to be incomplete: at least one
	// time sub-range hit maxFacetEntriesPerRange, or its query failed
	// partway through. Either way, the counts are a lower bound, not exact.
	Capped bool
}

const (
	// facetRangeConcurrency bounds how many time sub-ranges are tallied at
	// once — the same overlapping-latency role histogramBucketConcurrency
	// plays for Histogram (client.readLimiter, not this, is what actually
	// bounds request *rate*).
	facetRangeConcurrency = histogramBucketConcurrency

	// maxFacetEntriesPerRange caps how many entries a single sub-range's
	// tally will page through, mirroring histogramMaxEntriesPerBucket's
	// role: past this, that sub-range's contribution becomes a lower bound
	// (folded into FacetsResult.Capped) rather than paging a
	// pathologically high-volume range to completion. Sized so the total
	// across every sub-range (facetRangeConcurrency * this) lands around
	// 10,000 entries — a few seconds of paced requests at the default read
	// quota, representative for the vast majority of queries.
	maxFacetEntriesPerRange = 2500

	// facetPageSize is the page size used while paging a sub-range's
	// entries. Large, like histogramPageSize, since only Severity/LogName/
	// Resource/Labels are read from each entry here — Payload is never
	// materialized — so fewer round trips matters more than per-page
	// memory.
	facetPageSize int32 = 1000
)

// Facets computes a field-by-field breakdown (Severity, LogName, Resource,
// Labels) of every entry matching f across its full [Since, Until) range.
// f.Since and f.Until must both already be resolved, the same invariant
// Histogram documents.
//
// Cloud Logging has no native aggregate/count RPC (see Histogram's doc
// comment), so — exactly like Histogram — this splits the range into
// facetRangeConcurrency equal-width sub-ranges and pages each one fully,
// concurrently, paced by c.readLimiter, tallying fields off each entry as
// it streams by rather than materializing the whole entry. A sub-range
// that fails partway through, or hits maxFacetEntriesPerRange, never fails
// the whole call: its partial tally is kept and FacetsResult.Capped is
// set, mirroring HistogramBucket.Capped.
func (c *client) Facets(ctx context.Context, f FilterState) (FacetsResult, error) {
	if f.Since.IsZero() || f.Until.IsZero() {
		return FacetsResult{}, fmt.Errorf("facets: Since and Until must both be resolved (got Since=%v, Until=%v)", f.Since, f.Until)
	}

	ranges := bucketBounds(f.Since, f.Until, facetRangeConcurrency)

	aggs := make([]FacetAggregator, len(ranges))
	capped := make([]bool, len(ranges))

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(facetRangeConcurrency)
	for i := range ranges {
		g.Go(func() error {
			agg, rangeCapped, err := c.tallyFacetRange(gctx, f, ranges[i].Start, ranges[i].End)
			aggs[i] = agg
			// A failure and hitting the cap are folded into the same
			// Capped signal, same reasoning as Histogram: either way, this
			// sub-range's contribution isn't the full/exact answer.
			capped[i] = rangeCapped || err != nil
			return nil // never fails the group — see the doc comment above
		})
	}
	_ = g.Wait() // no goroutine above ever returns a non-nil error

	var merged FacetAggregator
	var anyCapped bool
	for i := range aggs {
		merged.Merge(aggs[i])
		anyCapped = anyCapped || capped[i]
	}
	return FacetsResult{Facets: merged.Result(), Capped: anyCapped}, nil
}

// tallyFacetRange pages through every entry matching f narrowed to
// [start, end), tallying Severity/LogName/Resource/Labels, up to
// maxFacetEntriesPerRange. Each page fetch is paced by c.readLimiter, one
// Wait per NextPage call — the same "one Wait per read request" accounting
// ListEntries and Histogram's tallyBucket both use.
func (c *client) tallyFacetRange(ctx context.Context, f FilterState, start, end time.Time) (agg FacetAggregator, capped bool, err error) {
	rf := f
	rf.Since, rf.Until = start, end

	it := c.admin.Entries(ctx, logadmin.Filter(rf.Build()))
	pager := iterator.NewPager(it, int(facetPageSize), "")

	var total int
	for {
		if err := c.readLimiter.Wait(ctx); err != nil {
			// Most commonly ctx expiring (facetsTimeout) while this
			// sub-range was still queued behind readLimiter's pacing, not
			// a real API failure — folded into Capped by the caller either
			// way (see Facets' doc comment).
			return agg, false, err
		}
		var page []*logging.Entry
		nextToken, err := pager.NextPage(&page)
		if err != nil {
			return agg, false, fmt.Errorf("listing log entries for project %q: %w", c.project, err)
		}
		for _, e := range page {
			resource := ""
			if e.Resource != nil {
				resource = e.Resource.Type
			}
			agg.add(e.Severity, e.LogName, resource, e.Labels)
			total++
			if total >= maxFacetEntriesPerRange {
				return agg, true, nil
			}
		}
		if nextToken == "" {
			return agg, false, nil
		}
	}
}
