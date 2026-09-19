package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
	"golang.org/x/text/encoding/korean"
)

func uploadImport(t *testing.T, env *testEnv, filename string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("form file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/visits/import/preview", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("X-CSRF-Token", env.csrf)
	request.RemoteAddr = "10.0.0.1:5000"
	for _, cookie := range env.cookies {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	env.handler.ServeHTTP(response, request)
	return response
}

func importedVisitors(t *testing.T, response *httptest.ResponseRecorder) ([]map[string]any, []any) {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("import returned %d: %s", response.Code, response.Body.String())
	}
	var body struct {
		Visitors []map[string]any `json:"visitors"`
		Warnings []any            `json:"warnings"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body.Visitors, body.Warnings
}

func TestVisitorImportAcceptsExcelExports(t *testing.T) {
	env := newTestEnv(t)
	header := "이름,휴대전화,회사명,반입장비,개인정보동의\n"
	rows := "홍길동,010-1234-5678,ABC테크,노트북;카메라,동의\n김철수,010-9876-5432,XYZ,,동의\n"

	// UTF-8 with the byte-order mark Excel writes for "CSV UTF-8".
	utf8CSV := append([]byte{0xEF, 0xBB, 0xBF}, []byte(header+rows)...)
	visitors, warnings := importedVisitors(t, uploadImport(t, env, "visitors.csv", utf8CSV))
	if len(visitors) != 2 || visitors[0]["name"] != "홍길동" || len(warnings) != 0 {
		t.Fatalf("UTF-8 import: %v / %v", visitors, warnings)
	}
	equipment, _ := visitors[0]["equipment"].([]any)
	if len(equipment) != 2 {
		t.Fatalf("equipment not split: %v", visitors[0]["equipment"])
	}

	// Korean Excel's default "CSV (쉼표로 분리)" is CP949, not UTF-8.
	cp949, err := korean.EUCKR.NewEncoder().Bytes([]byte(header + rows))
	if err != nil {
		t.Fatalf("encode cp949: %v", err)
	}
	visitors, _ = importedVisitors(t, uploadImport(t, env, "visitors.csv", cp949))
	if len(visitors) != 2 || visitors[0]["name"] != "홍길동" || visitors[0]["company"] != "ABC테크" {
		t.Fatalf("CP949 import: %v", visitors)
	}

	// XLSX straight from Excel. Rows 3 and 4 hold the phone as a number, which
	// is what Excel stores when the cell is typed without a leading apostrophe:
	// the leading 0 is gone, and a scientific number format turns it into
	// 1.012345678E+09. Both must come back as 01012345678 without a warning.
	book := excelize.NewFile()
	defer book.Close()
	sheet := book.GetSheetName(0)
	for index, row := range [][]string{
		{"이름", "휴대전화", "회사명", "개인정보동의"},
		{"이영희", "010-5555-6666", "델타", "동의"},
		{"박숫자", "", "델타", "동의"},
		{"최지수", "", "델타", "동의"},
	} {
		for column, value := range row {
			cell, _ := excelize.CoordinatesToCellName(column+1, index+1)
			_ = book.SetCellValue(sheet, cell, value)
		}
	}
	if err := book.SetCellValue(sheet, "B3", 1012345678); err != nil {
		t.Fatalf("numeric phone cell: %v", err)
	}
	scientific := "0.000000000E+00"
	style, err := book.NewStyle(&excelize.Style{CustomNumFmt: &scientific})
	if err != nil {
		t.Fatalf("scientific style: %v", err)
	}
	if err := book.SetCellValue(sheet, "B4", 1012345678); err != nil {
		t.Fatalf("numeric phone cell: %v", err)
	}
	if err := book.SetCellStyle(sheet, "B4", "B4", style); err != nil {
		t.Fatalf("scientific phone cell: %v", err)
	}
	var xlsx bytes.Buffer
	if err := book.Write(&xlsx); err != nil {
		t.Fatalf("write xlsx: %v", err)
	}
	visitors, warnings = importedVisitors(t, uploadImport(t, env, "visitors.xlsx", xlsx.Bytes()))
	if len(visitors) != 3 || visitors[0]["name"] != "이영희" || visitors[0]["phone"] != "010-5555-6666" {
		t.Fatalf("XLSX import: %v", visitors)
	}
	if visitors[1]["phone"] != "01012345678" || visitors[2]["phone"] != "01012345678" {
		t.Fatalf("numeric phone cells should be restored to 01012345678, got %v / %v", visitors[1]["phone"], visitors[2]["phone"])
	}
	if len(warnings) != 0 {
		t.Fatalf("restored phones must not warn, got %v", warnings)
	}

	// A missing consent column is a warning, not a rejection: the requester
	// still has to tick consent per visitor in the form.
	_, warnings = importedVisitors(t, uploadImport(t, env, "visitors.csv", []byte("이름,휴대전화\n박민수,010-1111-2222\n")))
	if len(warnings) != 1 {
		t.Fatalf("expected a consent warning, got %v", warnings)
	}

	// Rejections carry a message the requester can act on.
	for _, bad := range []struct{ name, content, expect string }{
		{"visitors.txt", "이름,휴대전화\n홍길동,01011112222\n", "지원 파일 형식"},
		{"visitors.csv", "회사명\nABC\n", "이름"},
		{"visitors.csv", "이름,휴대전화\n", "방문자 데이터"},
	} {
		response := uploadImport(t, env, bad.name, []byte(bad.content))
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), bad.expect) {
			t.Fatalf("%s returned %d: %s", bad.name, response.Code, response.Body.String())
		}
	}

	// No file at all is a request error, not a panic.
	var empty bytes.Buffer
	writer := multipart.NewWriter(&empty)
	_ = writer.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/visits/import/preview", &empty)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("X-CSRF-Token", env.csrf)
	for _, cookie := range env.cookies {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	env.handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("missing file returned %d", response.Code)
	}

	// The preview feeds straight into a visit request.
	visitors, _ = importedVisitors(t, uploadImport(t, env, "visitors.csv", utf8CSV))
	for _, visitor := range visitors {
		visitor["consent"] = true
	}
	created := env.json(http.MethodPost, "/api/v1/visits", visitBody(env.siteID(), map[string]any{"visitors": visitors}), http.StatusCreated)
	if fmt.Sprint(created["visitorCount"]) != "2" {
		t.Fatalf("imported visitors did not become a visit: %v", created)
	}
	var stored int
	if err := env.server.db.QueryRow(context.Background(), `SELECT count(*) FROM visitors WHERE company='ABC테크'`).Scan(&stored); err != nil || stored != 1 {
		t.Fatalf("company not stored as typed: %d %v", stored, err)
	}
}

func TestImportPhoneRestoresLeadingZeroFromNumericCells(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		// Excel stored the cell as a number and dropped the leading 0.
		{"1012345678", "01012345678"},
		{"3112345678", "03112345678"},
		{"21234567", "021234567"},
		// A scientific number format keeps every digit here, so it is restored.
		{"1.012345678E+09", "01012345678"},
		{"1.012345678e9", "01012345678"},
		{"1.012345678E9", "01012345678"},
		{"3.112345678E+09", "03112345678"},
		// Already a phone number as typed: untouched byte for byte.
		{"01012345678", "01012345678"},
		{"010-1234-5678", "010-1234-5678"},
		{"010 1234 5678", "010 1234 5678"},
		{"+82 10 1234 5678", "+82 10 1234 5678"},
		{"+821012345678", "+821012345678"},
		// Outside the 8-10 digit window: an international number without +, or
		// something too short to be a phone at all.
		{"21012345678", "21012345678"},
		{"821012345678", "821012345678"},
		{"2.1012345678E+10", "21012345678"},
		{"1234567", "1234567"},
		{"12", "12"},
		{"", ""},
		// A truncated mantissa cannot be restored: 1.01E+09 would become
		// 01010000000, a wrong number stored silently. Leave it for the warning.
		{"1.01E+09", "1.01E+09"},
		{"1.0123E+09", "1.0123E+09"},
		{"1.01234567E+09", "1.01234567E+09"},
		// Not an integer, or too large to be exact: untouched.
		{"1.0123456785E+09", "1.0123456785E+09"},
		{"1.012345678E+15", "1.012345678E+15"},
		{"1E+09", "1E+09"},
		{"abc", "abc"},
	} {
		if got := importPhone(tc.in); got != tc.want {
			t.Errorf("importPhone(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	// Through the parser: the numeric cell is stored restored and does not warn,
	// while other columns are untouched.
	visitors, warnings, err := visitorInputsFromRows([][]string{
		{"이름", "휴대전화", "회사명", "개인정보동의"},
		{"홍길동", "1012345678", "1012345678", "동의"},
		{"김철수", "1.012345678E+09", "ABC", "동의"},
		{"이영희", "1.01E+09", "ABC", "동의"},
	})
	if err != nil || len(visitors) != 3 {
		t.Fatalf("parse: %v %v", visitors, err)
	}
	if visitors[0].Phone != "01012345678" || visitors[1].Phone != "01012345678" || visitors[2].Phone != "1.01E+09" {
		t.Fatalf("phones: %q %q %q", visitors[0].Phone, visitors[1].Phone, visitors[2].Phone)
	}
	if visitors[0].Company != "1012345678" {
		t.Fatalf("company column must not be touched, got %q", visitors[0].Company)
	}
	if len(warnings) != 1 || !strings.HasPrefix(warnings[0], "행 4:") {
		t.Fatalf("only the truncated row should warn, got %v", warnings)
	}
}

func TestImportConsentReadsKoreanAndCheckMarks(t *testing.T) {
	for _, value := range []string{"y", "Yes", " TRUE ", "1", "O", "o", "ok", "V", "✓", "✔", "○", "ㅇ", "동의", "동의함", "동의합니다.", "예", "네", "확인", "완료", "체크", "있음"} {
		if !importConsent(value) {
			t.Errorf("%q should count as consent", value)
		}
	}
	for _, value := range []string{"", "n", "no", "false", "0", "x", "X", "아니오", "미동의", "동의안함", "거부", "-", "?", "보류"} {
		if importConsent(value) {
			t.Errorf("%q must not count as consent", value)
		}
	}
	// End to end through the parser: a sheet where the whole column is 예/O used
	// to raise one consent warning per visitor.
	visitors, warnings, err := visitorInputsFromRows([][]string{
		{"이름", "휴대전화", "개인정보동의"},
		{"홍길동", "010-1111-2222", "예"},
		{"김철수", "010-3333-4444", "O"},
		{"이영희", "010-5555-6666", "미동의"},
	})
	if err != nil || len(visitors) != 3 {
		t.Fatalf("parse: %v %v", visitors, err)
	}
	if !visitors[0].Consent || !visitors[1].Consent || visitors[2].Consent {
		t.Fatalf("consent flags: %v", visitors)
	}
	if len(warnings) != 1 || !strings.HasPrefix(warnings[0], "행 4:") {
		t.Fatalf("expected one warning for row 4, got %v", warnings)
	}
}

func TestImportFindsHeaderBelowTitleRows(t *testing.T) {
	// A template with a title, a blank line, then the real header. The row
	// numbers in warnings still match what the requester sees in Excel.
	rows := [][]string{
		{"2026년 9월 협력사 방문 명단"},
		{},
		{"이름", "휴대전화", "회사명", "개인정보동의"},
		{"홍길동", "010-1111-2222", "ABC", "동의"},
		{"김철수", "12", "ABC", "동의"},
		{"", "", "", ""},
		{"이영희", "010-5555-6666", "ABC", ""},
	}
	visitors, warnings, err := visitorInputsFromRows(rows)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(visitors) != 3 || visitors[0].Name != "홍길동" || visitors[0].Company != "ABC" || visitors[2].Name != "이영희" {
		t.Fatalf("visitors: %v", visitors)
	}
	if len(warnings) != 2 || !strings.HasPrefix(warnings[0], "행 5:") || !strings.HasPrefix(warnings[1], "행 7:") {
		t.Fatalf("warnings should quote Excel rows 5 and 7, got %v", warnings)
	}

	// The first row that has both required columns wins, so a normal sheet
	// still reads its first row as the header and numbers rows from 2.
	_, warnings, err = visitorInputsFromRows([][]string{{"이름", "휴대전화"}, {"홍길동", "1"}})
	if err != nil || len(warnings) != 2 || !strings.HasPrefix(warnings[0], "행 2:") {
		t.Fatalf("plain sheet: %v %v", warnings, err)
	}

	// A header found only after the scan limit is not used, and the rejection
	// still names the column missing from the first row.
	deep := make([][]string, 0, importHeaderScanLimit+2)
	for i := 0; i < importHeaderScanLimit; i++ {
		deep = append(deep, []string{"제목 " + strconv.Itoa(i)})
	}
	deep = append(deep, []string{"이름", "휴대전화"}, []string{"홍길동", "010-1111-2222"})
	if _, _, err := visitorInputsFromRows(deep); err == nil || !strings.Contains(err.Error(), "이름(name)") {
		t.Fatalf("header past the scan limit should be rejected, got %v", err)
	}

	// A title row followed by the header and nothing else is still "no data".
	if _, _, err := visitorInputsFromRows([][]string{{"명단"}, {"이름", "휴대전화"}}); err == nil || !strings.Contains(err.Error(), "방문자 데이터") {
		t.Fatalf("header without rows should be rejected, got %v", err)
	}

	// A row that has only one of the two required columns is not a header.
	if _, _, err := visitorInputsFromRows([][]string{{"이름", "회사명"}, {"홍길동", "ABC"}}); err == nil || !strings.Contains(err.Error(), "휴대전화(phone)") {
		t.Fatalf("missing phone column should be reported, got %v", err)
	}
}
