package gcplog

import (
	"context"
	"errors"
)

// TailEntries is implemented in milestone M5 (live tail), backed by the
// apiv2 LoggingClient's TailLogEntries streaming RPC. Until then it reports
// clearly that tail mode isn't available yet rather than silently no-oping.
func (c *client) TailEntries(ctx context.Context, f FilterState) (<-chan TailEvent, func(), error) {
	return nil, nil, errors.New("live tail is not implemented yet")
}
