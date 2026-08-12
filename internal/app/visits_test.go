package app

import "testing"

func TestNormalizeAndMaskPhone(t *testing.T) {
	if got := normalizePhone("+82 (0)10-1234-5678"); got != "8201012345678" {
		t.Fatalf("normalizePhone() = %q", got)
	}
	if got := maskPhone("010-1234-5678"); got != "010-****-5678" {
		t.Fatalf("maskPhone() = %q", got)
	}
}

func TestParseQRValue(t *testing.T) {
	raw, window, signature := parseQRValue("https://visit.example/q/vfq_example?ts=42&sig=abcd")
	if raw != "vfq_example" || window != "42" || signature != "abcd" {
		t.Fatalf("unexpected parsed qr: %q %q %q", raw, window, signature)
	}
	raw, window, signature = parseQRValue("vfq_plain")
	if raw != "vfq_plain" || window != "" || signature != "" {
		t.Fatalf("unexpected raw token parse: %q %q %q", raw, window, signature)
	}
}

func TestSiteScope(t *testing.T) {
	if !siteAllowed(User{Role: RoleLobby}, "hq") {
		t.Fatal("empty lobby scope must allow all sites")
	}
	if !siteAllowed(User{Role: RoleLobby, SiteScope: []string{"hq"}}, "hq") {
		t.Fatal("matching lobby scope rejected")
	}
	if siteAllowed(User{Role: RoleLobby, SiteScope: []string{"lab"}}, "hq") {
		t.Fatal("non-matching lobby scope allowed")
	}
	if !siteAllowed(User{Role: RoleSecurity, SiteScope: []string{"lab"}}, "hq") {
		t.Fatal("security role should not inherit lobby-only scope restriction")
	}
}

func TestRenderTemplate(t *testing.T) {
	got := renderTemplate("{{visitor}} / {{place}}", map[string]string{"visitor": "홍길동", "place": "본사"})
	if got != "홍길동 / 본사" {
		t.Fatalf("renderTemplate() = %q", got)
	}
}

func TestVisitorImportRows(t *testing.T) {
	rows := [][]string{
		{"\ufeff이름", "휴대전화", "회사명", "반입장비", "개인정보동의"},
		{"홍길동", "010-1234-5678", "ABC테크", "노트북; 카메라", "동의"},
		{"김철수", "010-9876-5432", "XYZ", "", ""},
	}
	visitors, warnings, err := visitorInputsFromRows(rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(visitors) != 2 || visitors[0].Name != "홍길동" || len(visitors[0].Equipment) != 2 || !visitors[0].Consent {
		t.Fatalf("unexpected import: %#v", visitors)
	}
	if len(warnings) != 1 || visitors[1].Consent {
		t.Fatalf("expected consent warning: %#v", warnings)
	}
}

func TestSettingValidation(t *testing.T) {
	valid := map[string]string{
		"visit.dynamic_qr_seconds":        "30",
		"security.api_key_allowed_scopes": "read write mcp",
		"notification.provider":           "webhook",
		"notification.webhook_url":        "https://sms.intra/api/send",
	}
	for key, value := range valid {
		if message := validateSettingValue(key, value); message != "" {
			t.Fatalf("%s=%s rejected: %s", key, value, message)
		}
	}
	invalid := map[string]string{
		"visit.dynamic_qr_seconds":        "10",
		"security.api_key_allowed_scopes": "read owner",
		"notification.provider":           "unknown",
	}
	for key, value := range invalid {
		if message := validateSettingValue(key, value); message == "" {
			t.Fatalf("%s=%s should be rejected", key, value)
		}
	}
}
