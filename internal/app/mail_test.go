package app

import (
	"bufio"
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hkjang/visitflow/internal/platform"
)

// fakeSMTP is a minimal cleartext SMTP server that records the DATA payload of
// every message, enough to exercise the sender and the mail-driven flows.
type fakeSMTP struct {
	listener net.Listener
	mu       sync.Mutex
	messages []string
	auths    []string
}

func startFakeSMTP(t *testing.T) *fakeSMTP {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := &fakeSMTP{listener: listener}
	go server.serve()
	t.Cleanup(func() { listener.Close() })
	return server
}

func (f *fakeSMTP) port() int { return f.listener.Addr().(*net.TCPAddr).Port }

func (f *fakeSMTP) serve() {
	for {
		conn, err := f.listener.Accept()
		if err != nil {
			return
		}
		go f.handle(conn)
	}
}

func (f *fakeSMTP) handle(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	write := func(line string) { _, _ = conn.Write([]byte(line + "\r\n")) }
	write("220 fake ESMTP")
	var data strings.Builder
	inData := false
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if inData {
			if line == "." {
				inData = false
				f.mu.Lock()
				f.messages = append(f.messages, data.String())
				f.mu.Unlock()
				data.Reset()
				write("250 queued")
				continue
			}
			data.WriteString(strings.TrimPrefix(line, ".") + "\n")
			continue
		}
		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "EHLO"):
			_, _ = conn.Write([]byte("250-fake\r\n250-AUTH LOGIN PLAIN\r\n250 OK\r\n"))
		case strings.HasPrefix(upper, "HELO"):
			write("250 fake")
		case strings.HasPrefix(upper, "AUTH LOGIN"):
			write("334 VXNlcm5hbWU6")
			user, _ := reader.ReadString('\n')
			write("334 UGFzc3dvcmQ6")
			pass, _ := reader.ReadString('\n')
			f.mu.Lock()
			f.auths = append(f.auths, strings.TrimSpace(user)+":"+strings.TrimSpace(pass))
			f.mu.Unlock()
			write("235 ok")
		case strings.HasPrefix(upper, "AUTH PLAIN"):
			write("235 ok")
		case strings.HasPrefix(upper, "MAIL FROM"), strings.HasPrefix(upper, "RCPT TO"):
			write("250 ok")
		case upper == "DATA":
			inData = true
			write("354 go")
		case upper == "QUIT":
			write("221 bye")
			return
		default:
			write("250 ok")
		}
	}
}

func (f *fakeSMTP) last() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.messages) == 0 {
		return ""
	}
	return f.messages[len(f.messages)-1]
}

func (f *fakeSMTP) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.messages)
}

func (f *fakeSMTP) all() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.messages...)
}

// "auto" takes STARTTLS when the relay offers it and otherwise stays in the
// clear, so the standard default works against a plain port-25 relay; an
// explicit "starttls" against the same relay is an error the operator sees.
func TestSendMailAutoSecurityFollowsTheRelay(t *testing.T) {
	relay := startFakeSMTP(t)
	cfg := platform.SMTPConfig{Host: "127.0.0.1", Port: relay.port(), From: "visitflow@test.local", Security: "auto", Username: "relay", Password: "relay-secret", Timeout: 5 * time.Second}
	if err := platform.SendMail(context.Background(), cfg, platform.Mail{To: []string{"ops@test.local"}, Subject: "auto", Body: "hello"}); err != nil {
		t.Fatalf("auto against a cleartext relay: %v", err)
	}
	if relay.count() != 1 {
		t.Fatalf("relay received %d messages", relay.count())
	}
	relay.mu.Lock()
	auths := relay.auths
	relay.mu.Unlock()
	// Without TLS the password must not go out as PLAIN; LOGIN was offered.
	if len(auths) != 1 || !strings.Contains(auths[0], "cmVsYXk=") {
		t.Fatalf("expected LOGIN authentication in the clear, got %v", auths)
	}
	cfg.Security = "starttls"
	err := platform.SendMail(context.Background(), cfg, platform.Mail{To: []string{"ops@test.local"}, Subject: "starttls", Body: "hello"})
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("starttls against a relay without it: %v", err)
	}
	cfg.Security = "ssl"
	if err := cfg.Validate(); err == nil {
		t.Fatal("unknown security mode accepted")
	}
}

func TestMailFromHeader(t *testing.T) {
	for _, tc := range []struct{ address, name, want string }{
		{"visitflow@company.intra", "", "<visitflow@company.intra>"},
		{"visitflow@company.intra", "VisitFlow", "\"VisitFlow\" <visitflow@company.intra>"},
		{" visitflow@company.intra ", " 방문 안내 ", "=?utf-8?q?=EB=B0=A9=EB=AC=B8_=EC=95=88=EB=82=B4?= <visitflow@company.intra>"},
		{"", "VisitFlow", ""},
	} {
		if got := mailFromHeader(tc.address, tc.name); got != tc.want {
			t.Fatalf("mailFromHeader(%q, %q) = %q, want %q", tc.address, tc.name, got, tc.want)
		}
	}
}

func TestMailSettingValidation(t *testing.T) {
	for key, value := range map[string]string{"mail.security": "AUTO", "mail.from_address": "visitflow@company.intra", "mail.from_name": "VisitFlow 안내", "mail.notify_checked_in": "false", "mail.timeout_seconds": "30", "mail.base_url": "https://visit.company.intra"} {
		if message := validateSettingValue(key, value); message != "" {
			t.Fatalf("%s=%q rejected: %s", key, value, message)
		}
	}
	for key, value := range map[string]string{"mail.security": "ssl", "mail.from_address": "VisitFlow <visitflow@company.intra>", "mail.from_name": "a<b>", "mail.notify_checked_in": "yes", "mail.timeout_seconds": "0", "mail.base_url": "visit.company.intra", "mail.smtp_port": "70000"} {
		if message := validateSettingValue(key, value); message == "" {
			t.Fatalf("%s=%q accepted", key, value)
		}
	}
}

func TestMaskEmail(t *testing.T) {
	for input, want := range map[string]string{"hong@company.intra": "h***@company.intra", "a@b": "***", "": "***"} {
		if got := maskEmail(input); got != want {
			t.Fatalf("maskEmail(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestParseMailPreferencesDefaultsAndOverrides(t *testing.T) {
	prefs := parseMailPreferences([]byte(`{}`))
	if !prefs.Enabled || !prefs.Events["checked_in"] || prefs.Events["checked_out"] {
		t.Fatalf("defaults wrong: %+v", prefs)
	}
	prefs = parseMailPreferences([]byte(`{"emailEnabled":false,"events":{"checked_out":true,"bogus":true}}`))
	if prefs.Enabled || !prefs.Events["checked_out"] {
		t.Fatalf("overrides not applied: %+v", prefs)
	}
	if _, leaked := prefs.Events["bogus"]; leaked {
		t.Fatal("unknown event key was kept")
	}
}
