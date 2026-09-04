package gcplog

import (
	"context"
	"errors"
	"fmt"

	"cloud.google.com/go/logging"
	"cloud.google.com/go/logging/logadmin"
	"google.golang.org/api/iterator"
)

func (c *client) ListEntries(ctx context.Context, f FilterState, pageToken string, pageSize int32) (Page, error) {
	it := c.admin.Entries(ctx,
		logadmin.Filter(f.Build()),
		logadmin.NewestFirst(),
	)

	pager := iterator.NewPager(it, int(pageSize), pageToken)
	var sdkEntries []*logging.Entry
	nextToken, err := pager.NextPage(&sdkEntries)
	if err != nil && !errors.Is(err, iterator.Done) {
		return Page{}, fmt.Errorf("listing log entries for project %q: %w", c.project, err)
	}
	// iterator.Pager.NextPage surfaces iterator.Done as an error rather
	// than an empty page when the underlying query matches zero entries
	// from the very first call (as opposed to running out of items after
	// some were already returned, which it handles fine) — a filter
	// that legitimately matches nothing is not a failure.

	entries := make([]Entry, len(sdkEntries))
	for i, e := range sdkEntries {
		entries[i] = entryFromSDK(e)
	}
	return Page{Entries: entries, NextPageToken: nextToken}, nil
}
