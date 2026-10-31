// Package email sends transactional messages. It exposes a small Sender
// interface so the application can fall back to logging during development and
// use SMTP in production without changing callers.
package email

import (
	"bytes"
	"context"
	"fmt"
	"mime"
	"net/mail"
	"strings"
)

// Message is a single transactional email.
type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
}

// Sender delivers a Message.
type Sender interface {
	Send(ctx context.Context, msg Message) error
}

// address extracts the bare email address from a value such as
// `Business OS <no-reply@example.com>`. Invalid input is returned trimmed.
func address(value string) string {
	value = strings.TrimSpace(value)
	if parsed, err := mail.ParseAddress(value); err == nil {
		return parsed.Address
	}
	return value
}

// buildRFC5322 renders a Message as an RFC 5322 payload. When HTML is present
// the message is multipart/alternative; otherwise it is plain text.
func buildRFC5322(from string, msg Message) ([]byte, error) {
	to := address(msg.To)
	if to == "" {
		return nil, fmt.Errorf("email: missing recipient")
	}
	subject := mime.QEncoding.Encode("utf-8", msg.Subject)
	fromHeader := strings.TrimSpace(from)
	if fromHeader == "" {
		fromHeader = to
	}

	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\n", fromHeader)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	fmt.Fprintf(&b, "MIME-Version: 1.0\r\n")

	if strings.TrimSpace(msg.HTML) == "" {
		fmt.Fprintf(&b, "Content-Type: text/plain; charset=utf-8\r\n")
		fmt.Fprintf(&b, "Content-Transfer-Encoding: 8bit\r\n\r\n")
		b.WriteString(strings.ReplaceAll(msg.Text, "\n", "\r\n"))
		return b.Bytes(), nil
	}

	boundary := "businessos-alt-boundary"
	fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", boundary)
	fmt.Fprintf(&b, "--%s\r\n", boundary)
	fmt.Fprintf(&b, "Content-Type: text/plain; charset=utf-8\r\n\r\n")
	b.WriteString(strings.ReplaceAll(msg.Text, "\n", "\r\n"))
	fmt.Fprintf(&b, "\r\n--%s\r\n", boundary)
	fmt.Fprintf(&b, "Content-Type: text/html; charset=utf-8\r\n\r\n")
	b.WriteString(msg.HTML)
	fmt.Fprintf(&b, "\r\n--%s--\r\n", boundary)
	return b.Bytes(), nil
}
