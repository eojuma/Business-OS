package email

import (
	"context"
	"log"
)

// LogSender writes messages to the application log instead of sending them.
// It is used when SMTP is not configured (development/test).
type LogSender struct {
	Logger *log.Logger
}

// NewLog builds a LogSender. A nil logger uses the standard logger.
func NewLog(logger *log.Logger) *LogSender {
	if logger == nil {
		logger = log.Default()
	}
	return &LogSender{Logger: logger}
}

// Send implements Sender.
func (s *LogSender) Send(_ context.Context, msg Message) error {
	s.Logger.Printf("email (SMTP not configured): to=%q subject=%q\n%s", msg.To, msg.Subject, msg.Text)
	return nil
}
