package gcplog

import (
	"testing"
	"time"

	"cloud.google.com/go/logging"
)

func TestClassifySeverity(t *testing.T) {
	cases := []struct {
		sev  logging.Severity
		want SeverityTier
	}{
		{logging.Default, SeverityTierDefault},
		{logging.Debug, SeverityTierDefault},
		{logging.Info, SeverityTierInfo},
		{logging.Notice, SeverityTierInfo},
		{logging.Warning, SeverityTierWarning},
		{logging.Error, SeverityTierError},
		{logging.Critical, SeverityTierError},
		{logging.Alert, SeverityTierAlert},
		{logging.Emergency, SeverityTierAlert},
	}
	for _, tc := range cases {
		if got := ClassifySeverity(tc.sev); got != tc.want {
			t.Errorf("ClassifySeverity(%v) = %v, want %v", tc.sev, got, tc.want)
		}
	}
}

func TestBucketBounds(t *testing.T) {
	since := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	until := since.Add(time.Hour)

	buckets := bucketBounds(since, until, 4)
	if len(buckets) != 4 {
		t.Fatalf("len(buckets) = %d, want 4", len(buckets))
	}

	if !buckets[0].Start.Equal(since) {
		t.Errorf("buckets[0].Start = %v, want %v", buckets[0].Start, since)
	}
	if !buckets[len(buckets)-1].End.Equal(until) {
		t.Errorf("last bucket's End = %v, want %v (pinned exactly, no rounding gap)", buckets[len(buckets)-1].End, until)
	}

	// Contiguous: each bucket's End is the next bucket's Start.
	for i := 0; i < len(buckets)-1; i++ {
		if !buckets[i].End.Equal(buckets[i+1].Start) {
			t.Errorf("bucket %d End = %v, bucket %d Start = %v, want equal (contiguous)", i, buckets[i].End, i+1, buckets[i+1].Start)
		}
	}

	// Equal width for a duration that divides evenly.
	want := 15 * time.Minute
	for i, b := range buckets {
		if got := b.End.Sub(b.Start); got != want {
			t.Errorf("bucket %d width = %v, want %v", i, got, want)
		}
	}
}

func TestBucketBoundsUnevenDivisionNoGap(t *testing.T) {
	// A span that doesn't divide evenly by n exercises the rounding case
	// bucketBounds guards against: the last bucket must still end exactly
	// at until, not at since+width*n, which integer duration division would
	// otherwise leave short of it.
	since := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	until := since.Add(100 * time.Millisecond)

	buckets := bucketBounds(since, until, 7)
	if !buckets[len(buckets)-1].End.Equal(until) {
		t.Errorf("last bucket's End = %v, want %v", buckets[len(buckets)-1].End, until)
	}
}

func TestHistogramBucketTotal(t *testing.T) {
	b := HistogramBucket{Counts: [NumSeverityTiers]int64{
		SeverityTierDefault: 3,
		SeverityTierError:   2,
	}}
	if got := b.Total(); got != 5 {
		t.Errorf("Total() = %d, want 5", got)
	}
}
