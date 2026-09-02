package app

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestParseAuditLogFilters(t *testing.T) {
	request := httptest.NewRequest("GET", "/api/v1/admin/audit-logs.csv?action=+visit.+&actor=+kim+&from=2026-01-02T03:04:05Z&to=2026-02-03T04:05:06Z", nil)
	filters := parseAuditLogFilters(request)
	if filters.action != "visit." || filters.actor != "kim" {
		t.Fatalf("filters did not trim the text fields: %+v", filters)
	}
	if filters.from == nil || !filters.from.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatalf("unexpected from: %v", filters.from)
	}
	if filters.to == nil || !filters.to.Equal(time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)) {
		t.Fatalf("unexpected to: %v", filters.to)
	}
	details := filters.details()
	if details["action"] != "visit." || details["actor"] != "kim" || details["from"] != "2026-01-02T03:04:05Z" || details["to"] != "2026-02-03T04:05:06Z" {
		t.Fatalf("export audit details do not describe the scope: %v", details)
	}
}

// An unparsable or absent period must widen to "no bound" rather than silently
// becoming a zero timestamp that would drop every row from the export.
func TestParseAuditLogFiltersIgnoresUnusableTimes(t *testing.T) {
	request := httptest.NewRequest("GET", "/api/v1/admin/audit-logs.csv?from=2026-01-02&to=", nil)
	filters := parseAuditLogFilters(request)
	if filters.from != nil || filters.to != nil {
		t.Fatalf("expected no period bounds, got from=%v to=%v", filters.from, filters.to)
	}
	if details := filters.details(); details["from"] != nil || details["to"] != nil {
		t.Fatalf("unbounded export should not claim a period: %v", details)
	}
}
