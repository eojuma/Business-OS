package email

import "github.com/businessos/backend/internal/config"

// NewFromConfig returns an SMTP sender when SMTP_HOST is set, otherwise a
// log-only sender for development.
func NewFromConfig(cfg *config.Config) Sender {
	if cfg != nil && cfg.SMTPHost != "" {
		return NewSMTP(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPFrom, cfg.SMTPUsername, cfg.SMTPPassword)
	}
	return NewLog(nil)
}
