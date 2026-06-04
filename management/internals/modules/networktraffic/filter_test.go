package networktraffic

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func TestFilterParseFromRequestNetworkOnly(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test?network_only=true&aggregate_flows=true&internal_dns=true", nil)

	var filter Filter
	filter.ParseFromRequest(req)

	if assert.NotNil(t, filter.NetworkOnly) {
		assert.True(t, *filter.NetworkOnly)
	}
	if assert.NotNil(t, filter.AggregateFlows) {
		assert.True(t, *filter.AggregateFlows)
	}
	if assert.NotNil(t, filter.InternalDNS) {
		assert.True(t, *filter.InternalDNS)
	}
}

func TestFilterNormalizeDateRangeDefaultsToLast15Days(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)

	var filter Filter
	filter.normalizeDateRange(now)

	assert.Equal(t, now, *filter.EndDate)
	assert.Equal(t, now.Add(-15*24*time.Hour), *filter.StartDate)
}

func TestFilterNormalizeDateRangeCapsLongRanges(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	startDate := now.Add(-30 * 24 * time.Hour)
	endDate := now.Add(-2 * time.Hour)

	filter := Filter{StartDate: &startDate, EndDate: &endDate}
	filter.normalizeDateRange(now)

	assert.Equal(t, endDate, *filter.EndDate)
	assert.Equal(t, endDate.Add(-15*24*time.Hour), *filter.StartDate)
}

func TestFilterNormalizeDateRangeKeepsShortRanges(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	startDate := now.Add(-2 * time.Hour)
	endDate := now.Add(-time.Hour)

	filter := Filter{StartDate: &startDate, EndDate: &endDate}
	filter.normalizeDateRange(now)

	assert.Equal(t, endDate, *filter.EndDate)
	assert.Equal(t, startDate, *filter.StartDate)
}

func TestFilterNormalizeDateRangeStartAfterEnd(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	startDate := now.Add(time.Hour)
	endDate := now

	filter := Filter{StartDate: &startDate, EndDate: &endDate}
	filter.normalizeDateRange(now)

	assert.Equal(t, endDate, *filter.EndDate)
	assert.Equal(t, endDate, *filter.StartDate)
}
