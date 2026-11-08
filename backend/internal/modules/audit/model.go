package audit

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// JSON stores an arbitrary audit snapshot in PostgreSQL jsonb.
type JSON json.RawMessage

// Value implements driver.Valuer so GORM writes valid jsonb.
func (j JSON) Value() (driver.Value, error) {
	if len(j) == 0 || string(j) == "null" {
		return nil, nil
	}
	return string(j), nil
}

// Scan implements sql.Scanner for jsonb columns.
func (j *JSON) Scan(value any) error {
	switch v := value.(type) {
	case nil:
		*j = nil
	case []byte:
		*j = append((*j)[:0], v...)
	case string:
		*j = JSON(v)
	default:
		return fmt.Errorf("audit: unsupported JSON value %T", value)
	}
	return nil
}

// MarshalJSON keeps the stored JSON document intact in API responses.
func (j JSON) MarshalJSON() ([]byte, error) {
	if len(j) == 0 || string(j) == "null" {
		return []byte("null"), nil
	}
	return j, nil
}

// IsZero reports whether the snapshot is absent.
func (j JSON) IsZero() bool { return len(j) == 0 || string(j) == "null" }

// Entry is one append-only audit record.
type Entry struct {
	ID          uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	BusinessID  uuid.UUID  `gorm:"type:uuid;index;not null" json:"business_id"`
	ActorUserID *uuid.UUID `gorm:"type:uuid;index" json:"actor_user_id,omitempty"`
	ActorName   string     `json:"actor_name,omitempty"`
	ActorEmail  string     `json:"actor_email,omitempty"`
	Action      string     `gorm:"not null" json:"action"`
	EntityType  string     `gorm:"not null" json:"entity_type"`
	EntityID    *uuid.UUID `gorm:"type:uuid" json:"entity_id,omitempty"`
	Before      JSON       `gorm:"type:jsonb" json:"before,omitempty"`
	After       JSON       `gorm:"type:jsonb" json:"after,omitempty"`
	Method      string     `json:"method"`
	Path        string     `json:"path"`
	StatusCode  int        `json:"status_code"`
	IPAddress   string     `json:"ip_address"`
	CreatedAt   time.Time  `gorm:"index" json:"created_at"`
}

func (Entry) TableName() string { return "audit_logs" }
