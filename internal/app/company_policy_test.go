package app

import (
	"net/http"
	"testing"
)

func TestCompanyRequiredPolicy(t *testing.T) {
	env := newTestEnv(t)
	reference := env.json(http.MethodGet, "/api/v1/reference-data", nil, http.StatusOK)
	if value, ok := reference["companyRequired"].(bool); !ok || value {
		t.Fatalf("default companyRequired = %#v, want boolean false", reference["companyRequired"])
	}
	siteID := env.siteID()
	hostID := reference["hosts"].([]any)[0].(map[string]any)["id"]
	for _, policy := range []string{"false", "true", "false"} {
		env.json(http.MethodPut, "/api/v1/settings", map[string]any{"settings": map[string]string{"visit.company_required": policy}}, http.StatusOK)
		reference = env.json(http.MethodGet, "/api/v1/reference-data", nil, http.StatusOK)
		if value, ok := reference["companyRequired"].(bool); !ok || value != (policy == "true") {
			t.Fatalf("policy %s: companyRequired = %#v", policy, reference["companyRequired"])
		}
		for _, path := range []string{"/api/v1/visits", "/api/v1/lobby/walk-ins"} {
			for _, company := range []string{"", " \t ", "테스트상사"} {
				body := visitBody(siteID, map[string]any{"hostUserId": hostID, "visitors": []map[string]any{
					{"name": "대표", "phone": "01012345678", "company": "정상회사", "consent": true},
					{"name": "동행", "phone": "01098765432", "company": company, "consent": true},
				}})
				want := http.StatusCreated
				if policy == "true" && company != "테스트상사" {
					want = http.StatusBadRequest
				}
				result := env.json(http.MethodPost, path, body, want)
				if want == http.StatusBadRequest && result["error"].(map[string]any)["code"] != "company_required" {
					t.Fatalf("wrong rejection: %#v", result)
				}
			}
		}
	}
	// Legacy/missing settings are fixtures; all policy transitions above use the real API.
	for _, value := range []string{"TRUE", " true ", ""} {
		if _, err := env.server.db.Exec(t.Context(), `UPDATE settings SET value=$1 WHERE key='visit.company_required'`, value); err != nil {
			t.Fatal(err)
		}
		env.server.invalidateSettings()
		result := env.json(http.MethodGet, "/api/v1/reference-data", nil, http.StatusOK)
		if result["companyRequired"] != false {
			t.Fatalf("legacy %q: %#v", value, result["companyRequired"])
		}
	}
	if _, err := env.server.db.Exec(t.Context(), `DELETE FROM settings WHERE key='visit.company_required'`); err != nil {
		t.Fatal(err)
	}
	env.server.invalidateSettings()
	if result := env.json(http.MethodGet, "/api/v1/reference-data", nil, http.StatusOK); result["companyRequired"] != false {
		t.Fatalf("missing key: %#v", result["companyRequired"])
	}
}
