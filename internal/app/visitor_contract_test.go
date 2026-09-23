package app

import (
	"net/http"
	"testing"
)

// The visit form checks visitor names and phones before submitting
// (web/src/visitors.ts). This pins the boundary it mirrors: the name is
// trimmed, the phone is counted with normalizePhone, and seven digits pass.
func TestVisitorNameAndPhoneBoundary(t *testing.T) {
	env := newTestEnv(t)
	siteID := env.siteID()
	for _, tc := range []struct {
		label string
		name  string
		phone string
		want  int
	}{
		{"정상", "김방문", "010-1234-5678", http.StatusCreated},
		{"앞뒤 공백은 다듬어 통과", "  김방문  ", "01012345678", http.StatusCreated},
		{"공백만 있는 이름", "   ", "01012345678", http.StatusBadRequest},
		{"빈 이름", "", "01012345678", http.StatusBadRequest},
		{"숫자 7자리", "김방문", "1234567", http.StatusCreated},
		{"숫자 6자리", "김방문", "123456", http.StatusBadRequest},
		{"구분자를 뺀 숫자 6자리", "김방문", "12-34-56", http.StatusBadRequest},
		{"숫자 없는 전화", "김방문", "연락처없음", http.StatusBadRequest},
		{"국가번호 표기", "김방문", "+82 10-1234-5678", http.StatusCreated},
	} {
		body := visitBody(siteID, map[string]any{"visitors": []map[string]any{
			{"name": tc.name, "phone": tc.phone, "company": "테스트상사", "consent": true},
		}})
		result := env.json(http.MethodPost, "/api/v1/visits", body, tc.want)
		if tc.want == http.StatusBadRequest {
			if code := result["error"].(map[string]any)["code"]; code != "invalid_visitor" {
				t.Fatalf("%s: rejected with %#v", tc.label, result)
			}
		}
	}
	// A bad companion is rejected too, which is why the form numbers the row.
	body := visitBody(siteID, map[string]any{"visitors": []map[string]any{
		{"name": "김방문", "phone": "01012345678", "company": "테스트상사", "consent": true},
		{"name": "이동행", "phone": "010", "company": "테스트상사", "consent": true},
	}})
	if result := env.json(http.MethodPost, "/api/v1/visits", body, http.StatusBadRequest); result["error"].(map[string]any)["code"] != "invalid_visitor" {
		t.Fatalf("companion: %#v", result)
	}
}
