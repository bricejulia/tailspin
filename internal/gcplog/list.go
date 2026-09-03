package gcplog

import (
	"context"
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
	if err != nil {
		return Page{}, fmt.Errorf("listing log entries for project %q: %w", c.project, err)
	}

	entries := make([]Entry, len(sdkEntries))
	for i, e := range sdkEntries {
		entries[i] = entryFromSDK(e)
	}
	return Page{Entries: entries, NextPageToken: nextToken}, nil
}
