package email

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// defaultSMTPTimeout bounds the whole SMTP dial + exchange when neither the
// caller nor the sender configures a shorter deadline.
const defaultSMTPTimeout = 15 * time.Second

// SMTPSender delivers messages through an SMTP relay.
//
// When RequireTLS is set (the SMTP_TLS default), the connection must offer
// STARTTLS and it is upgraded before AUTH or message data; delivery fails if
// the extension is missing. Plaintext is only used when RequireTLS is false.
type SMTPSender struct {
	Host       string
	Port       string
	From       string
	Username   string
	Password   string
	RequireTLS bool
	Timeout    time.Duration
}

// NewSMTP builds an SMTPSender.
func NewSMTP(host, port, from, username, password string, requireTLS bool) *SMTPSender {
	if strings.TrimSpace(port) == "" {
		port = "587"
	}
	return &SMTPSender{
		Host:       host,
		Port:       port,
		From:       from,
		Username:   username,
		Password:   password,
		RequireTLS: requireTLS,
	}
}

// Send implements Sender.
func (s *SMTPSender) Send(ctx context.Context, msg Message) error {
	if strings.TrimSpace(s.Host) == "" {
		return fmt.Errorf("email: SMTP host is not configured")
	}
	payload, err := buildRFC5322(s.From, msg)
	if err != nil {
		return err
	}

	timeout := s.Timeout
	if timeout <= 0 {
		timeout = defaultSMTPTimeout
	}
	deadline := time.Now().Add(timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}

	addr := net.JoinHostPort(s.Host, s.Port)
	dialer := &net.Dialer{Timeout: time.Until(deadline)}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("email: dial %s: %w", addr, err)
	}
	// Bound every subsequent SMTP command so a stalled relay cannot hang the
	// request thread indefinitely.
	_ = conn.SetDeadline(deadline)
	defer func() { _ = conn.Close() }()

	client, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		return fmt.Errorf("email: smtp client: %w", err)
	}
	defer func() { _ = client.Close() }()

	if s.RequireTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("email: relay %s does not support STARTTLS but SMTP_TLS is enabled", addr)
		}
		if err := client.StartTLS(&tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("email: starttls: %w", err)
		}
	}

	if s.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", s.Username, s.Password, s.Host)); err != nil {
			return fmt.Errorf("email: auth: %w", err)
		}
	}

	envelopeFrom := address(s.From)
	if envelopeFrom == "" {
		envelopeFrom = address(msg.To)
	}
	if err := client.Mail(envelopeFrom); err != nil {
		return fmt.Errorf("email: mail from: %w", err)
	}
	if err := client.Rcpt(address(msg.To)); err != nil {
		return fmt.Errorf("email: rcpt to: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("email: data: %w", err)
	}
	if _, err := writer.Write(payload); err != nil {
		_ = writer.Close()
		return fmt.Errorf("email: write message: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("email: close message: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("email: quit: %w", err)
	}
	return nil
}
