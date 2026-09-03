package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestFriendlyError(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantHint   string // substring expected in the result
		wantOrigin string // substring of the original message expected too
	}{
		{
			name:       "not found",
			err:        status.Error(codes.NotFound, "projects/bogus does not exist"),
			wantHint:   "Cloud Logging is enabled",
			wantOrigin: "does not exist",
		},
		{
			name:       "permission denied",
			err:        status.Error(codes.PermissionDenied, "caller does not have permission"),
			wantHint:   "roles/logging.viewer",
			wantOrigin: "does not have permission",
		},
		{
			name:       "unauthenticated",
			err:        status.Error(codes.Unauthenticated, "request had invalid authentication"),
			wantHint:   "gcloud auth application-default login",
			wantOrigin: "invalid authentication",
		},
		{
			name:       "wrapped grpc error still classified",
			err:        fmt.Errorf("listing entries: %w", status.Error(codes.PermissionDenied, "nope")),
			wantHint:   "roles/logging.viewer",
			wantOrigin: "nope",
		},
		{
			name:       "missing ADC",
			err:        errors.New("could not find default credentials. See https://... for more information"),
			wantHint:   "gcloud auth application-default login",
			wantOrigin: "could not find default credentials",
		},
		{
			name:       "unrecognized error passes through",
			err:        errors.New("something unexpected happened"),
			wantOrigin: "something unexpected happened",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := friendlyError(tc.err)
			if !strings.Contains(got, tc.wantOrigin) {
				t.Errorf("friendlyError(%v) = %q, want it to contain original message %q", tc.err, got, tc.wantOrigin)
			}
			if tc.wantHint != "" && !strings.Contains(got, tc.wantHint) {
				t.Errorf("friendlyError(%v) = %q, want it to contain hint %q", tc.err, got, tc.wantHint)
			}
		})
	}
}

func TestFriendlyErrorNil(t *testing.T) {
	if got := friendlyError(nil); got != "" {
		t.Errorf("friendlyError(nil) = %q, want empty string", got)
	}
}
