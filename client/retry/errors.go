package retry

import (
	"context"
	"errors"

	"github.com/cenkalti/backoff/v4"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Classification is the reconnect decision for an error.
type Classification int

const (
	Unknown Classification = iota
	Transient
	Permanent
	Graceful
)

// Classify returns the retry behavior for common client reconnect errors.
func Classify(err error) Classification {
	if err == nil {
		return Graceful
	}

	if errors.Is(err, context.Canceled) {
		return Graceful
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return Transient
	}

	s, ok := status.FromError(err)
	if !ok {
		return Unknown
	}

	switch s.Code() {
	case codes.Canceled:
		return Graceful
	case codes.InvalidArgument, codes.NotFound, codes.PermissionDenied, codes.Unauthenticated, codes.Unimplemented:
		return Permanent
	case codes.Aborted, codes.DeadlineExceeded, codes.Internal, codes.ResourceExhausted, codes.Unavailable:
		return Transient
	default:
		return Unknown
	}
}

func IsPermanent(err error) bool {
	return Classify(err) == Permanent
}

func IsPermissionDenied(err error) bool {
	if err == nil {
		return false
	}
	s, ok := status.FromError(err)
	return ok && s.Code() == codes.PermissionDenied
}

// PermanentIfClassified wraps known permanent errors so backoff stops immediately.
func PermanentIfClassified(err error) error {
	if IsPermanent(err) {
		return backoff.Permanent(err)
	}
	return err
}
