package gcplog

import (
	"context"
	"testing"
)

func TestNewClientRejectsNonPositiveReadQuota(t *testing.T) {
	// Validated before any network call is made, so this doesn't need real
	// credentials or a live project.
	for _, n := range []int{0, -1, -60} {
		if _, err := NewClient(context.Background(), "test-project", n); err == nil {
			t.Errorf("NewClient with readRequestsPerMinute=%d returned nil error, want an error", n)
		}
	}
}
