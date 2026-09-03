package tui

import (
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// friendlyError renders err with a short remediation hint appended for the
// handful of failure modes a user is actually likely to hit (missing ADC,
// permission/project problems, network hiccups), falling back to the raw
// error text otherwise. The underlying error is always shown too — this
// only adds context, never hides detail.
func friendlyError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()

	// ADC errors come from golang.org/x/oauth2/google, not a gRPC status,
	// so they're matched by substring rather than a code.
	if strings.Contains(msg, "could not find default credentials") {
		return msg + "\n\nrun: gcloud auth application-default login"
	}

	switch status.Code(err) {
	case codes.NotFound:
		return msg + "\n\nCheck the project ID is correct and that Cloud Logging is enabled for it."
	case codes.PermissionDenied:
		return msg + "\n\nYour account likely needs the \"Logs Viewer\" (roles/logging.viewer) IAM role on this project."
	case codes.Unauthenticated:
		return msg + "\n\nrun: gcloud auth application-default login"
	case codes.DeadlineExceeded, codes.Unavailable:
		return msg + "\n\nThe request timed out or the service is unreachable — check your network and try again (r to retry)."
	case codes.ResourceExhausted:
		return msg + "\n\nYou may be hitting a Cloud Logging API rate limit — wait a moment and try again."
	default:
		// Every other code (including OK, which can't reach here, and
		// non-gRPC errors, which status.Code maps to Unknown) falls
		// through to the raw message below.
	}

	return msg
}
