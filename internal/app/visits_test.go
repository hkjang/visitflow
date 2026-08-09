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
