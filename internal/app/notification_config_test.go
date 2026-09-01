package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func boolPointer(value bool) *bool { return &value }

func validNotificationAPIInput() notificationAPIInput {
	return notificationAPIInput{
		Name: "사내 SMS", Channel: "sms", BaseURL: "https://message.example/api", Path: "/v1/send",
		Method: http.MethodPost, RequestFormat: "json", Headers: map[string]string{"Authorization": "Bearer secret"},
		Parameters: map[string]string{"to": "{{recipient}}", "body": "{{message}}", "requestId": "{{idempotencyKey}}"}, SecretKeys: []string{"headers.Authorization"},
		TimeoutSeconds: 10, Enabled: boolPointer(true),
	}
}

func TestValidateNotificationAPIInput(t *testing.T) {
	in := validNotificationAPIInput()
	if message := validateNotificationAPIInput(in); message != "" {
		t.Fatalf("valid API rejected: %s", message)
	}
	in.Path = "https://attacker.example/send"
	if message := validateNotificationAPIInput(in); message == "" {
		t.Fatal("absolute path must be rejected")
	}
	in = validNotificationAPIInput()
	in.Method = http.MethodGet
	if message := validateNotificationAPIInput(in); message == "" {
		t.Fatal("GET with JSON body must be rejected")
	}
	in = validNotificationAPIInput()
	in.Headers = map[string]string{"Bad\nHeader": "value"}
	if message := validateNotificationAPIInput(in); message == "" {
		t.Fatal("invalid header name must be rejected")
	}
	in = validNotificationAPIInput()
	delete(in.Parameters, "requestId")
	if message := validateNotificationAPIInput(in); message == "" {
		t.Fatal("enabled configured API without an idempotency placeholder must be rejected")
	}
	in.Enabled = boolPointer(false)
	if message := validateNotificationAPIInput(in); message != "" {
		t.Fatalf("disabled API may be staged without idempotency: %s", message)
	}
	in.Enabled = boolPointer(true)
	in.Headers["X-Request-ID"] = "notification/{{notificationId}}"
	if message := validateNotificationAPIInput(in); message != "" {
		t.Fatalf("notificationId must satisfy idempotency validation: %s", message)
	}
}

func TestNotificationSecretMaskAndMerge(t *testing.T) {
	values := map[string]string{"Authorization": "Bearer real", "X-Tenant": "visitflow"}
	masked := maskNotificationSecrets(values, []string{"headers.Authorization"}, "headers")
	if masked["Authorization"] != maskedNotificationSecret || masked["X-Tenant"] != "visitflow" {
		t.Fatalf("unexpected mask result: %#v", masked)
	}
	merged, message := mergeNotificationSecrets(masked, values, []string{"headers.Authorization"}, "headers")
	if message != "" {
		t.Fatalf("masked update rejected: %s", message)
	}
	if merged["Authorization"] != "Bearer real" {
		t.Fatalf("masked update did not preserve secret: %#v", merged)
	}
	if _, message = mergeNotificationSecrets(masked, values, nil, "headers"); message == "" {
		t.Fatal("masked value must not become plaintext when its Secret Key is removed")
	}
	if _, message = mergeNotificationSecrets(map[string]string{"Missing": maskedNotificationSecret}, values, []string{"headers.Missing"}, "headers"); message == "" {
		t.Fatal("masked value without a previous secret must be rejected")
	}
}

func TestRenderNotificationTemplateStrict(t *testing.T) {
	got, err := renderNotificationTemplate("{{visitor}} / {{qrcodeUrl}}", map[string]string{"visitor": "홍길동", "qrcodeUrl": "https://visit.example/img/visitor/code.jpg"})
	if err != nil || got != "홍길동 / https://visit.example/img/visitor/code.jpg" {
		t.Fatalf("unexpected rendered template: %q, %v", got, err)
	}
	if _, err := renderNotificationTemplate("{{unknown}}", map[string]string{}); err == nil {
		t.Fatal("unknown placeholder must be rejected")
	}
	if _, err := renderNotificationTemplate("{{visitor}}", map[string]string{}); err == nil {
		t.Fatal("missing placeholder value must be rejected")
	}
}

func TestBuildNotificationJSONRequest(t *testing.T) {
	config := notificationAPIConfig{
		BaseURL: "https://message.example/api", Path: "/mms/send", Method: http.MethodPost, RequestFormat: "json",
		Headers:    map[string]string{"Authorization": "Bearer token", "X-Idempotency-Key": "{{idempotencyKey}}"},
		Parameters: map[string]string{"phone": "{{recipient}}", "text": "{{message}}", "image": "{{qrcodeUrl}}"},
	}
	item := claimedNotification{ID: "notification-1", Recipient: "01012345678", Message: "방문 안내", Channel: "mms", Metadata: map[string]string{"qrcodeUrl": "https://visit.example/img/visitor/code.jpg"}}
	req, err := buildNotificationRequest(context.Background(), config, item)
	if err != nil {
		t.Fatal(err)
	}
	if req.URL.String() != "https://message.example/api/mms/send" {
		t.Fatalf("unexpected endpoint: %s", req.URL)
	}
	if req.Header.Get("X-Idempotency-Key") != item.ID || req.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("unexpected headers: %#v", req.Header)
	}
	body, _ := io.ReadAll(req.Body)
	var payload map[string]string
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["phone"] != item.Recipient || payload["image"] != item.Metadata["qrcodeUrl"] {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestBuildNotificationQueryRequest(t *testing.T) {
	config := notificationAPIConfig{BaseURL: "https://talk.example", Path: "/send", Method: http.MethodGet, RequestFormat: "query", Headers: map[string]string{}, Parameters: map[string]string{"to": "{{recipient}}", "text": "{{message}}"}}
	item := claimedNotification{ID: "n-2", Recipient: "010 1234", Message: "A&B", Channel: "kakao", Metadata: map[string]string{}}
	req, err := buildNotificationRequest(context.Background(), config, item)
	if err != nil {
		t.Fatal(err)
	}
	if req.URL.Query().Get("to") != item.Recipient || req.URL.Query().Get("text") != item.Message {
		t.Fatalf("query parameters were not encoded: %s", req.URL.RawQuery)
	}
	if strings.Contains(req.URL.RawQuery, "A&B") {
		t.Fatalf("query value was not escaped: %s", req.URL.RawQuery)
	}
}

func TestCheckedOutVisitIDsDeduplicates(t *testing.T) {
	items := []automaticCheckoutItem{
		{visitID: "visit-1", participantID: "participant-1"},
		{visitID: "visit-1", participantID: "participant-2"},
		{visitID: "visit-2", participantID: "participant-3"},
	}
	got := checkedOutVisitIDs(items)
	if len(got) != 2 || got[0] != "visit-1" || got[1] != "visit-2" {
		t.Fatalf("unexpected visit IDs: %#v", got)
	}
}

func TestNotificationDispatchLockRequiresOwnedSendingClaim(t *testing.T) {
	normalized := strings.ToLower(strings.Join(strings.Fields(notificationDispatchLockSQL), " "))
	for _, required := range []string{
		"status='sending'",
		"claim_token=$2",
		"for update",
	} {
		if !strings.Contains(normalized, required) {
			t.Fatalf("dispatch lock is missing %q: %s", required, normalized)
		}
	}
}
