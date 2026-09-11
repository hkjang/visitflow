package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
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

	// XLSX straight from Excel.
	book := excelize.NewFile()
	defer book.Close()
	sheet := book.GetSheetName(0)
	for index, row := range [][]string{
		{"이름", "휴대전화", "회사명", "개인정보동의"},
		{"이영희", "010-5555-6666", "델타", "동의"},
	} {
		for column, value := range row {
			cell, _ := excelize.CoordinatesToCellName(column+1, index+1)
			_ = book.SetCellValue(sheet, cell, value)
		}
	}
	var xlsx bytes.Buffer
	if err := book.Write(&xlsx); err != nil {
		t.Fatalf("write xlsx: %v", err)
	}
	visitors, _ = importedVisitors(t, uploadImport(t, env, "visitors.xlsx", xlsx.Bytes()))
	if len(visitors) != 1 || visitors[0]["name"] != "이영희" {
		t.Fatalf("XLSX import: %v", visitors)
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
