package app

import (
	"bytes"
	"image/jpeg"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/hkjang/visitflow/internal/platform"
)

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

func TestStaticQRSignatureWorksWithDynamicPolicy(t *testing.T) {
	keys, err := platform.NewKeyringFromSecret(strings.Repeat("01", 32))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{keys: keys}
	raw := "vfq_static_example"
	value := s.staticQRURL("https://visit.example", raw)
	parsedRaw, marker, signature := parseQRValue(value)
	if parsedRaw != raw || marker != "static" || signature == "" {
		t.Fatalf("unexpected static qr url: %q %q %q", parsedRaw, marker, signature)
	}
	if !s.validateQRSignature(parsedRaw, marker, signature, 30, time.Unix(1_700_000_000, 0)) {
		t.Fatal("server-issued static signature must work while dynamic QR is enabled")
	}
	if s.validateQRSignature(parsedRaw+"x", marker, signature, 30, time.Unix(1_700_000_000, 0)) {
		t.Fatal("static signature must be bound to the raw QR token")
	}
	if s.validateQRSignature(parsedRaw, marker, signature+"x", 30, time.Unix(1_700_000_000, 0)) {
		t.Fatal("tampered static signature was accepted")
	}
}

func TestEncodeQRJPEG(t *testing.T) {
	data, err := encodeQRJPEG("https://visit.example/q/vfq_example")
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 3 || data[0] != 0xff || data[1] != 0xd8 || data[2] != 0xff {
		t.Fatal("encoded QR is not a JPEG")
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if config.Width != 560 || config.Height != 560 {
		t.Fatalf("unexpected QR dimensions: %dx%d", config.Width, config.Height)
	}
}

func TestQRCodeFileSeqValidation(t *testing.T) {
	if !validQRCodeFileSeq("Abcd_efgh-1234567890xyza") {
		t.Fatal("valid opaque QR file sequence was rejected")
	}
	for _, value := range []string{"short", "1234567890123456789012345", "../../etc/passwd.........", "12345678901234567890123."} {
		if validQRCodeFileSeq(value) {
			t.Fatalf("invalid QR file sequence accepted: %q", value)
		}
	}
}

func TestQRParticipantTerminalStatus(t *testing.T) {
	for _, status := range []string{"CHECKED_OUT", "CANCELLED", "REJECTED", "NO_SHOW"} {
		if !qrParticipantStatusTerminal(status) {
			t.Fatalf("terminal participant status accepted: %s", status)
		}
	}
	for _, status := range []string{"PENDING_APPROVAL", "SCHEDULED", "APPROVED", "ARRIVED", "CHECKED_IN"} {
		if qrParticipantStatusTerminal(status) {
			t.Fatalf("active participant status rejected: %s", status)
		}
	}
}

func TestQRAvailableForNotification(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	future := now.Add(time.Hour)
	past := now.Add(-time.Second)
	revoked := now.Add(-time.Minute)
	if !qrAvailableForNotification("SCHEDULED", &future, nil, now) {
		t.Fatal("active unexpired QR was rejected")
	}
	for name, test := range map[string]struct {
		status                string
		validUntil, revokedAt *time.Time
	}{
		"missing":     {status: "SCHEDULED"},
		"expired":     {status: "SCHEDULED", validUntil: &past},
		"revoked":     {status: "SCHEDULED", validUntil: &future, revokedAt: &revoked},
		"checked-out": {status: "CHECKED_OUT", validUntil: &future},
		"cancelled":   {status: "CANCELLED", validUntil: &future},
		"rejected":    {status: "REJECTED", validUntil: &future},
		"no-show":     {status: "NO_SHOW", validUntil: &future},
	} {
		if qrAvailableForNotification(test.status, test.validUntil, test.revokedAt, now) {
			t.Fatalf("%s QR was accepted for notification", name)
		}
	}
}

func TestReservedImagePathDoesNotFallbackToSPA(t *testing.T) {
	server := NewServer(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), fstest.MapFS{
		"index.html": {Data: []byte("spa")},
	}, "test", "test", "test")
	for _, target := range []string{"/img/unknown/path", "/img/visitor/short.jpg"} {
		request := httptest.NewRequest(http.MethodGet, target, nil)
		response := httptest.NewRecorder()
		server.Routes().ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s returned %d instead of 404", target, response.Code)
		}
		if strings.Contains(response.Body.String(), "spa") {
			t.Fatalf("%s incorrectly fell back to the SPA", target)
		}
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
