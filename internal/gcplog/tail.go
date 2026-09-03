package gcplog

import (
	"context"
	"fmt"

	"cloud.google.com/go/logging/apiv2/loggingpb"
)

// tailEventBuffer bounds how many entries can be queued between the stream
// goroutine and a slower consumer before the goroutine blocks on send.
const tailEventBuffer = 64

// TailEntries opens a live tail stream via the Logging API v2's
// TailLogEntries RPC (gRPC-only — the REST transport doesn't support it).
// The returned channel receives one TailEvent per entry as they arrive and
// is closed after a terminal TailEvent.Err; cancel stops the stream early
// (its own closing of the channel is still delivered as the terminal
// event, driven by ctx.Err()).
func (c *client) TailEntries(ctx context.Context, f FilterState) (<-chan TailEvent, func(), error) {
	ctx, cancel := context.WithCancel(ctx)

	stream, err := c.stream.TailLogEntries(ctx)
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("starting tail stream for project %q: %w", c.project, err)
	}

	req := &loggingpb.TailLogEntriesRequest{
		ResourceNames: []string{"projects/" + c.project},
		Filter:        f.Build(),
	}
	if err := stream.Send(req); err != nil {
		cancel()
		return nil, nil, fmt.Errorf("starting tail stream for project %q: %w", c.project, err)
	}

	events := make(chan TailEvent, tailEventBuffer)
	go pumpTailEntries(ctx, stream, events)
	return events, cancel, nil
}

func pumpTailEntries(ctx context.Context, stream loggingpb.LoggingServiceV2_TailLogEntriesClient, events chan<- TailEvent) {
	defer close(events)
	for {
		resp, err := stream.Recv()
		if err != nil {
			sendTailEvent(ctx, events, TailEvent{Err: fmt.Errorf("tail stream ended: %w", err)})
			return
		}
		for _, le := range resp.GetEntries() {
			entry, err := entryFromProto(le)
			if err != nil {
				continue // skip an unparseable entry rather than killing the stream over it
			}
			if !sendTailEvent(ctx, events, TailEvent{Entry: entry}) {
				return
			}
		}
	}
}

// sendTailEvent sends ev, returning false if ctx was cancelled first (the
// caller should stop pumping in that case).
func sendTailEvent(ctx context.Context, events chan<- TailEvent, ev TailEvent) bool {
	select {
	case events <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}
