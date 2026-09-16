package app

import (
	"bytes"
	"encoding/csv"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/korean"

	"github.com/xuri/excelize/v2"
)

const maxVisitorImportSize = 6 << 20

func (s *Server) previewVisitorImport(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxVisitorImportSize)
	if err := r.ParseMultipartForm(maxVisitorImportSize); err != nil {
		writeError(w, http.StatusBadRequest, "import_too_large", "CSV/XLSX 파일은 6MB 이하여야 합니다")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file_required", "가져올 CSV 또는 XLSX 파일을 선택하세요")
		return
	}
	defer file.Close()

	rows, err := readVisitorImportRows(file, header)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_import", err.Error())
		return
	}
	visitors, warnings, err := visitorInputsFromRows(rows)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_import", err.Error())
		return
	}
	u, _ := userFrom(r)
	s.audit(r.Context(), u.ID, "visitor_import.preview", "visitor_import", "", clientIP(r), map[string]any{"filename": filepath.Base(header.Filename), "rows": len(visitors), "warnings": len(warnings)})
	writeJSON(w, http.StatusOK, map[string]any{"visitors": visitors, "warnings": warnings})
}

func readVisitorImportRows(file multipart.File, header *multipart.FileHeader) ([][]string, error) {
	ext := strings.ToLower(filepath.Ext(header.Filename))
	switch ext {
	case ".csv":
		data, err := io.ReadAll(file)
		if err != nil {
			return nil, errors.New("CSV 파일을 읽을 수 없습니다")
		}
		reader := csv.NewReader(bytes.NewReader(decodeSpreadsheetText(data)))
		reader.FieldsPerRecord = -1
		reader.LazyQuotes = true
		rows, err := reader.ReadAll()
		if err != nil {
			return nil, errors.New("CSV 형식을 읽을 수 없습니다")
		}
		return rows, nil
	case ".xlsx":
		book, err := excelize.OpenReader(file)
		if err != nil {
			return nil, errors.New("XLSX 형식을 읽을 수 없습니다")
		}
		defer book.Close()
		sheets := book.GetSheetList()
		if len(sheets) == 0 {
			return nil, errors.New("XLSX에 워크시트가 없습니다")
		}
		rows, err := book.GetRows(sheets[0])
		if err != nil {
			return nil, errors.New("XLSX 첫 번째 워크시트를 읽을 수 없습니다")
		}
		return rows, nil
	default:
		return nil, errors.New("지원 파일 형식은 .csv와 .xlsx입니다")
	}
}

// decodeSpreadsheetText normalises what Excel actually produces. Korean Windows
// saves "CSV (쉼표로 분리)" in CP949, not UTF-8, so a file exported from Excel
// arrived as mojibake and failed with "이름(name) 열이 필요합니다" even though the
// column was there. A UTF-8 byte-order mark is stripped for the same reason.
func decodeSpreadsheetText(data []byte) []byte {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if utf8.Valid(data) {
		return data
	}
	decoded, err := korean.EUCKR.NewDecoder().Bytes(data)
	if err != nil {
		return data
	}
	return decoded
}

// importHeaderAliases maps the column names people actually type, lower-cased
// with spaces, underscores and hyphens removed, onto the visitor fields.
var importHeaderAliases = map[string]string{
	"name": "name", "이름": "name", "방문자이름": "name", "성명": "name",
	"phone": "phone", "mobile": "phone", "휴대전화": "phone", "전화번호": "phone", "휴대폰": "phone", "연락처": "phone",
	"email": "email", "이메일": "email",
	"company": "company", "회사": "company", "회사명": "company",
	"title": "title", "직책": "title",
	"vehicle": "vehicle", "차량번호": "vehicle",
	"equipment": "equipment", "반입장비": "equipment",
	"consent": "consent", "개인정보동의": "consent", "동의": "consent",
	"locale": "locale", "언어": "locale", "language": "locale",
}

// importHeaderScanLimit bounds how far down a sheet the header row is looked
// for, so a title line or two above the table does not reject the whole file.
const importHeaderScanLimit = 10

// importHeaders reads one row as the header line and returns the column index
// of every recognised field.
func importHeaders(row []string) map[string]int {
	headers := map[string]int{}
	for index, raw := range row {
		key := strings.TrimPrefix(strings.TrimSpace(raw), "\ufeff")
		key = strings.ToLower(strings.NewReplacer(" ", "", "_", "", "-", "").Replace(key))
		if canonical := importHeaderAliases[key]; canonical != "" {
			headers[canonical] = index
		}
	}
	return headers
}

// importHeaderRow finds the row that carries both required columns. Sheets
// exported from a shared template often start with a title or a blank line, so
// the first row is not always the header. When no row within the scan limit
// qualifies the first row is returned so the caller reports which column is
// missing from it.
func importHeaderRow(rows [][]string) (int, map[string]int) {
	for index, row := range rows {
		if index >= importHeaderScanLimit {
			break
		}
		headers := importHeaders(row)
		if _, hasName := headers["name"]; !hasName {
			continue
		}
		if _, hasPhone := headers["phone"]; !hasPhone {
			continue
		}
		return index, headers
	}
	return 0, importHeaders(rows[0])
}

// importConsent reads the consent column the way people fill it in: an English
// yes, a Korean 예/동의, or the O/V/✓ marks Excel users put in a check column.
func importConsent(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimRight(value, ".!。")
	switch value {
	case "y", "yes", "true", "t", "1", "o", "ok", "v", "✓", "✔", "☑", "○", "◯", "ㅇ",
		"동의", "동의함", "동의합니다", "동의완료", "예", "네", "확인", "완료", "체크", "있음", "함", "수집동의", "개인정보동의":
		return true
	}
	return false
}

func visitorInputsFromRows(rows [][]string) ([]VisitorInput, []string, error) {
	if len(rows) < 2 {
		return nil, nil, errors.New("헤더와 방문자 데이터가 필요합니다")
	}
	headerRow, headers := importHeaderRow(rows)
	if _, ok := headers["name"]; !ok {
		return nil, nil, errors.New("이름(name) 열이 필요합니다")
	}
	if _, ok := headers["phone"]; !ok {
		return nil, nil, errors.New("휴대전화(phone) 열이 필요합니다")
	}
	if len(rows) <= headerRow+1 {
		return nil, nil, errors.New("헤더와 방문자 데이터가 필요합니다")
	}
	// Warnings quote the row number the requester sees in Excel: the header is
	// row headerRow+1 and the first visitor is the row after it.
	rowNumber := func(rowIndex int) string { return strconv.Itoa(headerRow + rowIndex + 2) }
	cell := func(row []string, key string) string {
		index, ok := headers[key]
		if !ok || index >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[index])
	}
	visitors := make([]VisitorInput, 0, len(rows)-headerRow-1)
	warnings := []string{}
	for rowIndex, row := range rows[headerRow+1:] {
		name, phone := cell(row, "name"), cell(row, "phone")
		if name == "" && phone == "" {
			continue
		}
		if len(visitors) >= 100 {
			return nil, nil, errors.New("한 번에 최대 100명의 방문자를 가져올 수 있습니다")
		}
		consent := importConsent(cell(row, "consent"))
		if name == "" || len(normalizePhone(phone)) < 7 {
			warnings = append(warnings, "행 "+rowNumber(rowIndex)+": 이름 또는 휴대전화를 확인하세요")
		}
		if !consent {
			warnings = append(warnings, "행 "+rowNumber(rowIndex)+": 개인정보 동의 확인이 필요합니다")
		}
		equipmentText := cell(row, "equipment")
		equipment := []string{}
		for _, item := range strings.FieldsFunc(equipmentText, func(r rune) bool { return r == ',' || r == ';' || r == '|' }) {
			if item = strings.TrimSpace(item); item != "" {
				equipment = append(equipment, item)
			}
		}
		visitors = append(visitors, VisitorInput{Name: name, Phone: phone, Email: cell(row, "email"), Company: cell(row, "company"), Title: cell(row, "title"), Vehicle: cell(row, "vehicle"), Equipment: equipment, Locale: normalizeLocale(cell(row, "locale")), Consent: consent})
	}
	if len(visitors) == 0 {
		return nil, nil, errors.New("가져올 방문자 데이터가 없습니다")
	}
	return visitors, warnings, nil
}
