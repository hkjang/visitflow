package app

import (
	"encoding/csv"
	"errors"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

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
	s.audit(r.Context(), u.ID, "visitor_import.preview", "visitor_import", "", r.RemoteAddr, map[string]any{"filename": filepath.Base(header.Filename), "rows": len(visitors), "warnings": len(warnings)})
	writeJSON(w, http.StatusOK, map[string]any{"visitors": visitors, "warnings": warnings})
}

func readVisitorImportRows(file multipart.File, header *multipart.FileHeader) ([][]string, error) {
	ext := strings.ToLower(filepath.Ext(header.Filename))
	switch ext {
	case ".csv":
		reader := csv.NewReader(file)
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

func visitorInputsFromRows(rows [][]string) ([]VisitorInput, []string, error) {
	if len(rows) < 2 {
		return nil, nil, errors.New("헤더와 방문자 데이터가 필요합니다")
	}
	headers := map[string]int{}
	aliases := map[string]string{
		"name": "name", "이름": "name", "방문자이름": "name",
		"phone": "phone", "mobile": "phone", "휴대전화": "phone", "전화번호": "phone",
		"email": "email", "이메일": "email",
		"company": "company", "회사": "company", "회사명": "company",
		"title": "title", "직책": "title",
		"vehicle": "vehicle", "차량번호": "vehicle",
		"equipment": "equipment", "반입장비": "equipment",
		"consent": "consent", "개인정보동의": "consent", "동의": "consent",
	}
	for index, raw := range rows[0] {
		key := strings.TrimPrefix(strings.TrimSpace(raw), "\ufeff")
		key = strings.ToLower(strings.NewReplacer(" ", "", "_", "", "-", "").Replace(key))
		if canonical := aliases[key]; canonical != "" {
			headers[canonical] = index
		}
	}
	if _, ok := headers["name"]; !ok {
		return nil, nil, errors.New("이름(name) 열이 필요합니다")
	}
	if _, ok := headers["phone"]; !ok {
		return nil, nil, errors.New("휴대전화(phone) 열이 필요합니다")
	}
	cell := func(row []string, key string) string {
		index, ok := headers[key]
		if !ok || index >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[index])
	}
	visitors := make([]VisitorInput, 0, len(rows)-1)
	warnings := []string{}
	for rowIndex, row := range rows[1:] {
		name, phone := cell(row, "name"), cell(row, "phone")
		if name == "" && phone == "" {
			continue
		}
		if len(visitors) >= 100 {
			return nil, nil, errors.New("한 번에 최대 100명의 방문자를 가져올 수 있습니다")
		}
		consentValue := strings.ToLower(cell(row, "consent"))
		consent := consentValue == "y" || consentValue == "yes" || consentValue == "true" || consentValue == "1" || consentValue == "동의"
		if name == "" || len(normalizePhone(phone)) < 7 {
			warnings = append(warnings, "행 "+strconv.Itoa(rowIndex+2)+": 이름 또는 휴대전화를 확인하세요")
		}
		if !consent {
			warnings = append(warnings, "행 "+strconv.Itoa(rowIndex+2)+": 개인정보 동의 확인이 필요합니다")
		}
		equipmentText := cell(row, "equipment")
		equipment := []string{}
		for _, item := range strings.FieldsFunc(equipmentText, func(r rune) bool { return r == ',' || r == ';' || r == '|' }) {
			if item = strings.TrimSpace(item); item != "" {
				equipment = append(equipment, item)
			}
		}
		visitors = append(visitors, VisitorInput{Name: name, Phone: phone, Email: cell(row, "email"), Company: cell(row, "company"), Title: cell(row, "title"), Vehicle: cell(row, "vehicle"), Equipment: equipment, Consent: consent})
	}
	if len(visitors) == 0 {
		return nil, nil, errors.New("가져올 방문자 데이터가 없습니다")
	}
	return visitors, warnings, nil
}
