// Package gcplog is the seam between tailspin's TUI and GCP: it talks to
// Cloud Logging directly via the Go client libraries (never by shelling out
// to gcloud) and exposes a small, TUI-agnostic Client interface that can be
// faked in tests without any network access.
package gcplog

import (
	"context"
	"fmt"

	"cloud.google.com/go/logging/logadmin"
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
	Close() error
}

// client is the real Client implementation, backed by the Cloud Logging
// client libraries.
type client struct {
	project string
	admin   *logadmin.Client
}

// NewClient creates a Client for the given GCP project, using Application
// Default Credentials (the same credentials `gcloud auth
// application-default login` sets up).
func NewClient(ctx context.Context, project string) (Client, error) {
	admin, err := logadmin.NewClient(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("creating Cloud Logging client for project %q: %w", project, err)
	}
	return &client{project: project, admin: admin}, nil
}

func (c *client) Close() error {
	return c.admin.Close()
}
