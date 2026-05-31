package retry

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want Classification
	}{
		{name: "nil is graceful", err: nil, want: Graceful},
		{name: "context canceled is graceful", err: context.Canceled, want: Graceful},
		{name: "context deadline is transient", err: context.DeadlineExceeded, want: Transient},
		{name: "permission denied is permanent", err: status.Error(codes.PermissionDenied, "denied"), want: Permanent},
		{name: "invalid argument is permanent", err: status.Error(codes.InvalidArgument, "bad config"), want: Permanent},
		{name: "unavailable is transient", err: status.Error(codes.Unavailable, "down"), want: Transient},
		{name: "deadline exceeded grpc is transient", err: status.Error(codes.DeadlineExceeded, "slow"), want: Transient},
		{name: "plain error is unknown", err: errors.New("plain"), want: Unknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(tt.err); got != tt.want {
				t.Fatalf("Classify() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClassificationString(t *testing.T) {
	tests := []struct {
		classification Classification
		want           string
	}{
		{classification: Unknown, want: "unknown"},
		{classification: Transient, want: "transient"},
		{classification: Permanent, want: "permanent"},
		{classification: Graceful, want: "graceful"},
		{classification: Classification(99), want: "unknown"},
	}

	for _, tt := range tests {
		if got := tt.classification.String(); got != tt.want {
			t.Fatalf("String() = %q, want %q", got, tt.want)
		}
	}
}
