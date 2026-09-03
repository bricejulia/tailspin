// Package gcplogtest provides a fake gcplog.Client for tests, so
// internal/tui (and anything else that depends on gcplog.Client) can be
// exercised without a real GCP project or network access.
package gcplogtest

import (
	"context"

	"github.com/bricejulia/tailspin/internal/gcplog"
)

// Client is a fully scripted, in-memory gcplog.Client.
type Client struct {
	// Pages is returned from ListEntries in order, one page per call
	// (each fresh call — not per unique pageToken — advances the index,
	// which is enough to drive pagination in a test without needing to
	// model real page tokens).
	Pages   []gcplog.Page
	ListErr error

	// TailEvents is delivered, in order, on the channel TailEntries
	// returns, which is then closed.
	TailEvents []gcplog.TailEvent
	TailErr    error

	Closed bool

	listCalls int
}

var _ gcplog.Client = (*Client)(nil)

func (c *Client) ListEntries(_ context.Context, _ gcplog.FilterState, _ string, _ int32) (gcplog.Page, error) {
	if c.ListErr != nil {
		return gcplog.Page{}, c.ListErr
	}
	if c.listCalls >= len(c.Pages) {
		return gcplog.Page{}, nil
	}
	page := c.Pages[c.listCalls]
	c.listCalls++
	return page, nil
}

func (c *Client) TailEntries(_ context.Context, _ gcplog.FilterState) (<-chan gcplog.TailEvent, func(), error) {
	if c.TailErr != nil {
		return nil, nil, c.TailErr
	}
	events := make(chan gcplog.TailEvent, len(c.TailEvents))
	for _, ev := range c.TailEvents {
		events <- ev
	}
	close(events)
	return events, func() {}, nil
}

func (c *Client) Close() error {
	c.Closed = true
	return nil
}
