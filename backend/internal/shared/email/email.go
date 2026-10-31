// Package email sends transactional messages. It exposes a small Sender
// interface so the application can fall back to logging during development and
// use SMTP in production without changing callers.
package email

import (
	"bytes"
	"context"
	"encoding/base64"
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
// `Business OS <no-reply@example.com>`. Header-breaking characters are stripped
// so user-supplied names cannot inject additional headers.
func address(value string) string {
	value = strings.TrimSpace(value)
	if parsed, err := mail.ParseAddress(value); err == nil {
		return sanitizeHeader(parsed.Address)
	}
	return sanitizeHeader(value)
}

// sanitizeHeader removes CR/LF so a value cannot start a new header line.
func sanitizeHeader(value string) string {
	replacer := strings.NewReplacer("\r", "", "\n", "")
	return strings.TrimSpace(replacer.Replace(value))
}

// writeBase64Body writes body base64-encoded with 76-character lines. Base64
// output never contains a hyphen, so encoded content can never begin a MIME
// boundary delimiter even when the message embeds unvalidated text.
func writeBase64Body(b *bytes.Buffer, body string) {
	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	for len(encoded) > 76 {
		b.WriteString(encoded[:76])
		b.WriteString("\r\n")
		encoded = encoded[76:]
	}
	b.WriteString(encoded)
}

// buildRFC5322 renders a Message as an RFC 5322 payload. When HTML is present
// the message is multipart/alternative; otherwise it is plain text. Body parts
// are base64-encoded to prevent MIME-boundary injection.
func buildRFC5322(from string, msg Message) ([]byte, error) {
	to := address(msg.To)
	if to == "" {
		return nil, fmt.Errorf("email: missing recipient")
	}
	subject := mime.QEncoding.Encode("utf-8", sanitizeHeader(msg.Subject))
	fromHeader := sanitizeHeader(from)
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
		fmt.Fprintf(&b, "Content-Transfer-Encoding: base64\r\n\r\n")
		writeBase64Body(&b, msg.Text)
		return b.Bytes(), nil
	}

	boundary := "businessos-alt-boundary"
	fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", boundary)
	fmt.Fprintf(&b, "--%s\r\n", boundary)
	fmt.Fprintf(&b, "Content-Type: text/plain; charset=utf-8\r\n")
	fmt.Fprintf(&b, "Content-Transfer-Encoding: base64\r\n\r\n")
	writeBase64Body(&b, msg.Text)
	fmt.Fprintf(&b, "\r\n--%s\r\n", boundary)
	fmt.Fprintf(&b, "Content-Type: text/html; charset=utf-8\r\n")
	fmt.Fprintf(&b, "Content-Transfer-Encoding: base64\r\n\r\n")
	writeBase64Body(&b, msg.HTML)
	fmt.Fprintf(&b, "\r\n--%s--\r\n", boundary)
	return b.Bytes(), nil
}
