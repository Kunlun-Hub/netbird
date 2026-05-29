package client

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithDefaultPort(t *testing.T) {
	tests := []struct {
		name     string
		rawURL   string
		expected string
	}{
		{
			name:     "keeps explicit https port",
			rawURL:   "https://example.com:8443",
			expected: "example.com:8443",
		},
		{
			name:     "adds https port",
			rawURL:   "https://example.com",
			expected: "example.com:443",
		},
		{
			name:     "adds http port",
			rawURL:   "http://example.com",
			expected: "example.com:80",
		},
		{
			name:     "wraps ipv6 host",
			rawURL:   "https://[2001:db8::1]",
			expected: "[2001:db8::1]:443",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := url.Parse(tt.rawURL)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, withDefaultPort(parsed))
		})
	}
}
