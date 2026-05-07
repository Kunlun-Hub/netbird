package internal

import "testing"

func TestShouldSkipWGIfaceMonitor(t *testing.T) {
	tests := []struct {
		goos string
		want bool
	}{
		{goos: "android", want: true},
		{goos: "ios", want: true},
		{goos: "js", want: true},
		{goos: "linux", want: false},
		{goos: "windows", want: false},
		{goos: "darwin", want: false},
	}

	for _, tt := range tests {
		if got := shouldSkipWGIfaceMonitor(tt.goos); got != tt.want {
			t.Fatalf("shouldSkipWGIfaceMonitor(%q) = %v, want %v", tt.goos, got, tt.want)
		}
	}
}
