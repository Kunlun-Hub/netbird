package networktraffic

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultPageSize = 50
	MaxPageSize     = 500
	DefaultSortBy   = "timestamp"
	DefaultSortOrd  = "desc"
)

var validSortFields = map[string]string{
	"timestamp":   "timestamp",
	"protocol":    "protocol",
	"direction":   "direction",
	"type":        "event_type",
	"user_id":     "user_id",
	"reporter_id": "reporter_id",
}

type Filter struct {
	Page     int
	PageSize int
	SortBy   string
	SortOrd  string

	Search         *string
	UserID         *string
	ReporterID     *string
	Protocol       *int
	EventType      *string
	ConnectionType *string
	Direction      *string
	StartDate      *time.Time
	EndDate        *time.Time
}

func (f *Filter) ParseFromRequest(r *http.Request) {
	query := r.URL.Query()
	f.Page = parsePositiveInt(query.Get("page"), 1)
	f.PageSize = min(parsePositiveInt(query.Get("page_size"), DefaultPageSize), MaxPageSize)
	f.SortBy = parseSortField(query.Get("sort_by"))
	f.SortOrd = parseSortOrder(query.Get("sort_order"))
	f.Search = parseOptionalString(query.Get("search"))
	f.UserID = parseOptionalString(query.Get("user_id"))
	f.ReporterID = parseOptionalString(query.Get("reporter_id"))
	f.Protocol = parseOptionalInt(query.Get("protocol"))
	f.EventType = parseOptionalString(query.Get("type"))
	f.ConnectionType = parseOptionalString(query.Get("connection_type"))
	f.Direction = parseOptionalString(query.Get("direction"))
	f.StartDate = parseOptionalRFC3339(query.Get("start_date"))
	f.EndDate = parseOptionalRFC3339(query.Get("end_date"))
}

func (f *Filter) GetOffset() int {
	return (f.Page - 1) * f.PageSize
}

func (f *Filter) GetLimit() int {
	return f.PageSize
}

func (f *Filter) GetSortColumn() string {
	if field, ok := validSortFields[f.SortBy]; ok {
		return field
	}
	return validSortFields[DefaultSortBy]
}

func (f *Filter) GetSortOrder() string {
	if f.SortOrd == "asc" || f.SortOrd == "desc" {
		return f.SortOrd
	}
	return DefaultSortOrd
}

func parsePositiveInt(s string, defaultValue int) int {
	if s == "" {
		return defaultValue
	}
	if val, err := strconv.Atoi(s); err == nil && val > 0 {
		return val
	}
	return defaultValue
}

func parseOptionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func parseOptionalInt(s string) *int {
	if s == "" {
		return nil
	}
	if val, err := strconv.Atoi(s); err == nil {
		return &val
	}
	return nil
}

func parseOptionalRFC3339(s string) *time.Time {
	if s == "" {
		return nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return &t
	}
	return nil
}

func parseSortField(s string) string {
	if _, ok := validSortFields[s]; ok {
		return s
	}
	return DefaultSortBy
}

func parseSortOrder(s string) string {
	s = strings.ToLower(s)
	if s == "asc" || s == "desc" {
		return s
	}
	return DefaultSortOrd
}
