package tui

import (
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/logging"

	"github.com/bricejulia/tailspin/internal/gcplog"
)

// entries returns n throwaway entries, for tests that only care about count.
func entries(n int) []gcplog.Entry {
	out := make([]gcplog.Entry, n)
	for i := range out {
		out[i] = gcplog.Entry{Timestamp: time.Now(), Severity: logging.Info, Summary: "entry"}
	}
	return out
}

func TestTruncateMiddle(t *testing.T) {
	cases := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{name: "fits as-is", s: "short", n: 10, want: "short"},
		{name: "exact fit", s: "exactly10c", n: 10, want: "exactly10c"},
		{
			name: "elides the middle, keeps both ends",
			s:    "0123456789",
			n:    7,
			want: "012…789",
		},
		{name: "n of 1 just truncates", s: "abcdef", n: 1, want: "a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// "…" is itself a multi-byte rune, so — same as the existing
			// truncate helper — n bounds the budget the visible content
			// is cut to, not a strict cap on the result's byte length.
			if got := truncateMiddle(tc.s, tc.n); got != tc.want {
				t.Errorf("truncateMiddle(%q, %d) = %q, want %q", tc.s, tc.n, got, tc.want)
			}
		})
	}

	// The property this exists for: the tail of a long string survives,
	// unlike plain head-truncation.
	long := "resource.type=\"k8s_container\" AND timestamp>=\"2026-09-03T12:00:00Z\""
	got := truncateMiddle(long, 40)
	if !strings.HasSuffix(got, long[len(long)-15:]) {
		t.Errorf("truncateMiddle(%q, 40) = %q, want it to end with the original string's tail", long, got)
	}
}

func TestListModelPageNavigation(t *testing.T) {
	m := newListModel()
	m.reset(gcplog.Page{Entries: entries(50), NextPageToken: "tok1"})

	if got := m.pageNumber(); got != 1 {
		t.Fatalf("pageNumber = %d, want 1 right after reset", got)
	}
	if got := m.pageCount(); got != 1 {
		t.Fatalf("pageCount = %d, want 1 right after reset", got)
	}

	// On the only loaded page with more available: gotoNextPage should
	// report that a fetch is needed, flip loading, and NOT move
	// selection yet (that happens once appendPage lands).
	if needsFetch := m.gotoNextPage(); !needsFetch {
		t.Fatal("gotoNextPage() = false, want true (no next page loaded yet, but hasMore)")
	}
	if !m.loading {
		t.Error("loading = false after gotoNextPage needed a fetch")
	}
	if m.selected != 0 {
		t.Errorf("selected = %d, want 0 (unchanged until the fetch lands)", m.selected)
	}

	// Simulate the fetch landing.
	m.appendPage(gcplog.Page{Entries: entries(50), NextPageToken: ""})

	if m.selected != 50 {
		t.Errorf("selected = %d, want 50 (jumped to the new page's start)", m.selected)
	}
	if got := m.pageNumber(); got != 2 {
		t.Errorf("pageNumber = %d, want 2", got)
	}
	if got := m.pageCount(); got != 2 {
		t.Errorf("pageCount = %d, want 2", got)
	}
	if m.hasMore {
		t.Error("hasMore = true, want false (second page had no next token)")
	}

	// hasMore is now false: gotoNextPage should be a no-op.
	if needsFetch := m.gotoNextPage(); needsFetch {
		t.Error("gotoNextPage() = true on the last page with no more available, want false")
	}
	if m.selected != 50 {
		t.Errorf("selected = %d, want unchanged at 50", m.selected)
	}

	// gotoPreviousPage should be free (no fetch) and jump back to page 1.
	m.gotoPreviousPage()
	if m.selected != 0 {
		t.Errorf("selected = %d, want 0 after gotoPreviousPage", m.selected)
	}
	if got := m.pageNumber(); got != 1 {
		t.Errorf("pageNumber = %d, want 1", got)
	}

	// And forward again: since page 2 is already loaded, this should be
	// instant (no fetch) this time.
	if needsFetch := m.gotoNextPage(); needsFetch {
		t.Error("gotoNextPage() = true for an already-loaded page, want false")
	}
	if m.selected != 50 {
		t.Errorf("selected = %d, want 50", m.selected)
	}
}

func TestListModelGotoPreviousPageAtStart(t *testing.T) {
	m := newListModel()
	m.reset(gcplog.Page{Entries: entries(10)})
	m.selected = 5

	m.gotoPreviousPage()
	if m.selected != 0 {
		t.Errorf("selected = %d, want 0 (clamped to the start of the only page)", m.selected)
	}
}
