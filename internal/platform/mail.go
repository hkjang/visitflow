package platform

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

// SMTPConfig describes an on-premises relay. Corporate servers commonly speak
// STARTTLS on 587 with LOGIN authentication, so both are supported natively
// without an external dependency.
type SMTPConfig struct {
	Host          string
	Port          int
	Username      string
	Password      string
	From          string
	Security      string // "starttls", "tls" or "none"
	SkipTLSVerify bool
	Timeout       time.Duration
}

type Mail struct {
	To      []string
	Subject string
	Body    string
}

func (c SMTPConfig) Validate() error {
	if strings.TrimSpace(c.Host) == "" {
		return errors.New("SMTP 서버 주소가 필요합니다")
	}
	if c.Port < 1 || c.Port > 65535 {
		return errors.New("SMTP 포트는 1~65535 사이여야 합니다")
	}
	if _, err := mail.ParseAddress(c.From); err != nil {
		return errors.New("발신자 주소 형식을 확인하세요")
	}
	switch c.Security {
	case "starttls", "tls", "none":
	default:
		return errors.New("보안 방식은 starttls, tls, none 중 하나여야 합니다")
	}
	return nil
}

// loginAuth implements the LOGIN mechanism that Exchange and many appliances
// require; net/smtp only ships PLAIN and CRAM-MD5.
type loginAuth struct{ username, password string }

func (a *loginAuth) Start(*smtp.ServerInfo) (string, []byte, error) { return "LOGIN", nil, nil }
func (a *loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	switch strings.ToLower(strings.TrimSpace(string(fromServer))) {
	case "username:":
		return []byte(a.username), nil
	case "password:":
		return []byte(a.password), nil
	}
	return nil, errors.New("unexpected LOGIN challenge")
}

// SendMail delivers one message and returns a user-facing error on failure.
func SendMail(ctx context.Context, cfg SMTPConfig, message Mail) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if len(message.To) == 0 {
		return errors.New("수신자가 필요합니다")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	address := net.JoinHostPort(cfg.Host, fmt.Sprint(cfg.Port))
	dialer := &net.Dialer{Timeout: timeout}
	tlsConfig := &tls.Config{ServerName: cfg.Host, InsecureSkipVerify: cfg.SkipTLSVerify, MinVersion: tls.VersionTLS12} //nolint:gosec // operator-controlled for self-signed intranet relays
	var conn net.Conn
	var err error
	if cfg.Security == "tls" {
		conn, err = tls.DialWithDialer(dialer, "tcp", address, tlsConfig)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return fmt.Errorf("SMTP 서버에 연결하지 못했습니다: %w", err)
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("SMTP 인사에 실패했습니다: %w", err)
	}
	defer client.Close()
	if err := client.Hello("visitflow"); err != nil {
		return fmt.Errorf("SMTP EHLO 실패: %w", err)
	}
	if cfg.Security == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("SMTP 서버가 STARTTLS를 지원하지 않습니다")
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("STARTTLS 실패: %w", err)
		}
	}
	if cfg.Username != "" {
		if ok, mechanisms := client.Extension("AUTH"); ok {
			var auth smtp.Auth
			switch {
			case strings.Contains(mechanisms, "PLAIN") && cfg.Security != "none":
				auth = smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
			case strings.Contains(mechanisms, "LOGIN"):
				auth = &loginAuth{cfg.Username, cfg.Password}
			case strings.Contains(mechanisms, "CRAM-MD5"):
				auth = smtp.CRAMMD5Auth(cfg.Username, cfg.Password)
			case strings.Contains(mechanisms, "PLAIN"):
				// Plain credentials without TLS: allowed only because the operator
				// chose "none" for an isolated relay.
				auth = &plainInsecure{smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)}
			default:
				return fmt.Errorf("지원하는 SMTP 인증 방식이 없습니다: %s", mechanisms)
			}
			if err := client.Auth(auth); err != nil {
				return fmt.Errorf("SMTP 인증 실패: %w", err)
			}
		} else {
			return errors.New("SMTP 서버가 인증을 지원하지 않는데 계정이 설정되어 있습니다")
		}
	}
	from, _ := mail.ParseAddress(cfg.From)
	if err := client.Mail(from.Address); err != nil {
		return fmt.Errorf("발신자 거부: %w", err)
	}
	for _, to := range message.To {
		parsed, parseErr := mail.ParseAddress(to)
		if parseErr != nil {
			return fmt.Errorf("수신자 주소 형식을 확인하세요: %s", to)
		}
		if err := client.Rcpt(parsed.Address); err != nil {
			return fmt.Errorf("수신자 거부 (%s): %w", parsed.Address, err)
		}
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("본문 전송 시작 실패: %w", err)
	}
	if _, err := writer.Write([]byte(buildMessage(cfg.From, message))); err != nil {
		return fmt.Errorf("본문 전송 실패: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("본문 전송 종료 실패: %w", err)
	}
	return client.Quit()
}

// plainInsecure lets PLAIN run over a cleartext connection, which net/smtp
// otherwise refuses. Used only when the operator selected security "none".
type plainInsecure struct{ inner smtp.Auth }

func (a *plainInsecure) Start(info *smtp.ServerInfo) (string, []byte, error) {
	copyInfo := *info
	copyInfo.TLS = true
	return a.inner.Start(&copyInfo)
}
func (a *plainInsecure) Next(fromServer []byte, more bool) ([]byte, error) {
	return a.inner.Next(fromServer, more)
}

func buildMessage(from string, message Mail) string {
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + strings.Join(message.To, ", ") + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", message.Subject) + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	b.WriteString("X-Mailer: VisitFlow\r\n\r\n")
	// Dot-stuff lines that start with "." so the DATA terminator is not confused.
	for _, line := range strings.Split(strings.ReplaceAll(message.Body, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, ".") {
			line = "." + line
		}
		b.WriteString(line + "\r\n")
	}
	return b.String()
}
