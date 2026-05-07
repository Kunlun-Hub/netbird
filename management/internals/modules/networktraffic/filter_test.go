package networktraffic

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFilterParseFromRequestPageSize(t *testing.T) {
	tests := []struct {
		name             string
		pageSize         string
		expectedPageSize int
	}{
		{
			name:             "default page size",
			expectedPageSize: DefaultPageSize,
		},
		{
			name:             "valid page size",
			pageSize:         "1000",
			expectedPageSize: 1000,
		},
		{
			name:             "large page size is allowed for client-side grouping",
			pageSize:         "10000",
			expectedPageSize: MaxPageSize,
		},
		{
			name:             "page size exceeding max is capped",
			pageSize:         "10001",
			expectedPageSize: MaxPageSize,
		},
		{
			name:             "invalid page size falls back to default",
			pageSize:         "invalid",
			expectedPageSize: DefaultPageSize,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			q := req.URL.Query()
			if tt.pageSize != "" {
				q.Set("page_size", tt.pageSize)
			}
			req.URL.RawQuery = q.Encode()

			var filter Filter
			filter.ParseFromRequest(req)

			assert.Equal(t, tt.expectedPageSize, filter.PageSize)
		})
	}
}
