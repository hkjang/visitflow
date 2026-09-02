package app

import (
	"bytes"
	"encoding/csv"
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
