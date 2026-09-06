package app

import (
	"bytes"
	"encoding/csv"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestCSVCellNeutralizesFormulas(t *testing.T) {
	dangerous := []string{
		"=cmd|'/c calc'!A0",
		"+1+1",
		"-2+3",
		"@SUM(A1:A9)",
		"\t=HYPERLINK(\"http://attacker.example\")",
		"\r=1+1",
	}
	for _, value := range dangerous {
		got := csvCell(value)
		if got != "'"+value {
			t.Fatalf("csvCell(%q) = %q, want the value prefixed with a quote", value, got)
		}
	}
	harmless := []string{"", "테스트상사", "010-1234-5678", "VF-20260902-0001", `{"count":3}`, "회의 =중요", "2026-09-02 10:00"}
	for _, value := range harmless {
		if got := csvCell(value); got != value {
			t.Fatalf("csvCell(%q) = %q, want it unchanged", value, got)
		}
	}
}

func TestWriteCSVRowEscapesUntrustedValues(t *testing.T) {
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	writeCSVRow(writer, []string{"홍길동", "=cmd|'/c calc'!A0", "정상값"})
	writer.Flush()
	record, err := csv.NewReader(strings.NewReader(buffer.String())).Read()
	if err != nil {
		t.Fatalf("read back the written row: %v", err)
	}
	want := []string{"홍길동", "'=cmd|'/c calc'!A0", "정상값"}
	if !reflect.DeepEqual(record, want) {
		t.Fatalf("row = %q, want %q", record, want)
	}
}

func TestParseVisitExportFiltersReadsTheScreenFilters(t *testing.T) {
	filters := parseVisitExportFilters(httptest.NewRequest(http.MethodGet, "/admin/visits.csv?days=30&status=checked_in&q=%20테스트상사%20", nil))
	if filters.days != 30 {
		t.Fatalf("days = %d, want 30", filters.days)
	}
	if filters.status != "CHECKED_IN" {
		t.Fatalf("status = %q, want CHECKED_IN", filters.status)
	}
	if filters.search != "테스트상사" {
		t.Fatalf("search = %q, want the trimmed query", filters.search)
	}
	if details := filters.details(); details["days"] != 30 || details["status"] != "CHECKED_IN" || details["q"] != "테스트상사" {
		t.Fatalf("details() = %v, want the applied scope", details)
	}
}

func TestParseVisitExportFiltersRejectsUnusableValues(t *testing.T) {
	for _, query := range []string{"", "days=0", "days=4000", "days=abc"} {
		if filters := parseVisitExportFilters(httptest.NewRequest(http.MethodGet, "/admin/visits.csv?"+query, nil)); filters.days != 90 {
			t.Fatalf("parseVisitExportFilters(%q).days = %d, want the 90 day default", query, filters.days)
		}
	}
	// An unknown status must widen back to every status instead of filtering the
	// export down to nothing without telling the operator why.
	if filters := parseVisitExportFilters(httptest.NewRequest(http.MethodGet, "/admin/visits.csv?status=NOT_A_STATUS", nil)); filters.status != "" {
		t.Fatalf("status = %q, want it dropped", filters.status)
	}
}

// The export cap is what stands between a wide period and a response that pulls
// the whole table, so the request must never be able to lift it above the cap.
func TestParseVisitExportFiltersClampsTheRowLimit(t *testing.T) {
	for query, want := range map[string]int{
		"":             visitExportRowLimit,
		"limit=0":      visitExportRowLimit,
		"limit=-5":     visitExportRowLimit,
		"limit=abc":    visitExportRowLimit,
		"limit=999999": visitExportRowLimit,
		"limit=25":     25,
	} {
		if filters := parseVisitExportFilters(httptest.NewRequest(http.MethodGet, "/admin/visits.csv?"+query, nil)); filters.limit != want {
			t.Fatalf("parseVisitExportFilters(%q).limit = %d, want %d", query, filters.limit, want)
		}
	}
	if details := parseVisitExportFilters(httptest.NewRequest(http.MethodGet, "/admin/visits.csv?limit=25", nil)).details(); details["limit"] != 25 {
		t.Fatalf("details() = %v, want the applied row limit", details)
	}
}

// The notice is the only thing in a downloaded file that separates "these are
// all the rows" from "the rest was cut", so it has to survive a CSV round trip
// as a readable last line rather than an empty tail row.
func TestExportTruncationNoticeIsTheLastReadableRow(t *testing.T) {
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	writeCSVRow(writer, []string{"id", "value"})
	writeExportTruncationNotice(writer, 10000)
	writer.Flush()

	reader := csv.NewReader(strings.NewReader(buffer.String()))
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("read back the written rows: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("rows = %d, want the header and the notice", len(records))
	}
	notice := records[1][0]
	if !strings.HasPrefix(notice, "#") || !strings.Contains(notice, "10000") {
		t.Fatalf("notice = %q, want a commented line naming the limit", notice)
	}
}
