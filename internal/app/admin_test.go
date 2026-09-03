package app

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStatisticsSpanWhereDatesInTheSiteTimezone(t *testing.T) {
	clause := statisticsSpanWhere("v.start_at")
	if !strings.Contains(clause, "(v.start_at AT TIME ZONE si.timezone)::date BETWEEN (SELECT from_day FROM span) AND (SELECT to_day FROM span)") {
		t.Fatalf("span filter does not date the column in the site timezone: %s", clause)
	}
	if strings.Contains(clause, "CURRENT_DATE") {
		t.Fatalf("span filter fell back to the session date: %s", clause)
	}
	// The span CTE reads the day count from $1, so every query that embeds the
	// filter has to pass exactly that one parameter.
	if !strings.Contains(statisticsSpanCTE, "($1::int-1)") {
		t.Fatalf("span CTE no longer takes the day count as $1: %s", statisticsSpanCTE)
	}
}

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
