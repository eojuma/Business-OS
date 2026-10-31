package email

import (
	"context"
	"fmt"
	"net"
	"net/smtp"
	"strings"
)

// SMTPSender delivers messages through an SMTP relay. smtp.SendMail upgrades
// to TLS via STARTTLS automatically when the server advertises it (typically
// port 587). It uses only the standard library so no new dependency is needed.
type SMTPSender struct {
	Host     string
	Port     string
	From     string
	Username string
	Password string
}

// NewSMTP builds an SMTPSender.
func NewSMTP(host, port, from, username, password string) *SMTPSender {
	if strings.TrimSpace(port) == "" {
		port = "587"
	}
	return &SMTPSender{Host: host, Port: port, From: from, Username: username, Password: password}
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

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	addr := net.JoinHostPort(s.Host, s.Port)
	var auth smtp.Auth
	if s.Username != "" {
		auth = smtp.PlainAuth("", s.Username, s.Password, s.Host)
	}
	envelopeFrom := address(s.From)
	if envelopeFrom == "" {
		envelopeFrom = address(msg.To)
	}
	if err := smtp.SendMail(addr, auth, envelopeFrom, []string{address(msg.To)}, payload); err != nil {
		return fmt.Errorf("email: send via %s: %w", addr, err)
	}
	return nil
}
