package audit

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"time"

	sharedmw "github.com/businessos/backend/internal/shared/middleware"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ContextEntityIDKey lets a handler supply the audit entity id when the
// response does not expose it under a conventional key.
const ContextEntityIDKey = "audit_entity_id"

// SetEntityID records the entity id for the current request's audit entry.
func SetEntityID(c *gin.Context, id uuid.UUID) {
	c.Set(ContextEntityIDKey, id.String())
}

// Middleware records successful mutating requests from one shared hook.
func Middleware(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		action, entity, ok := Classify(c.Request.Method, c.FullPath())
		if !ok {
			c.Next()
			return
		}
		actorID := contextUUID(c, sharedmw.ContextUserIDKey)
		businessID := contextUUID(c, sharedmw.ContextBusinessIDKey)
		if businessID == nil {
			c.Next()
			return
		}

		entityID := pathEntityID(c, entity, *businessID)
		var before JSON
		if entityID != nil {
			before = snapshot(db, *businessID, entity, *entityID)
		}

		writer := &recordingWriter{ResponseWriter: c.Writer}
		c.Writer = writer
		c.Next()

		status := writer.Status()
		if status < http.StatusOK || status >= http.StatusMultipleChoices {
			return
		}
		after := responseData(writer.body.Bytes())
		if action == ActionReceiptCommit {
			if recordType := recordTypeFromJSON(after); recordType == "sale" || recordType == "purchase" {
				entity = recordType
			}
		}
		if override := contextUUID(c, ContextEntityIDKey); override != nil {
			entityID = override
		} else if entityID == nil {
			entityID = idFromJSON(after)
		}
		if action != ActionDelete && entityID != nil {
			if persisted := snapshot(db, *businessID, entity, *entityID); persisted != nil {
				after = persisted
			}
		}

		entry := Entry{
			BusinessID: *businessID,
			Action:     action,
			EntityType: entity,
			EntityID:   entityID,
			Before:     before,
			After:      after,
			Method:     c.Request.Method,
			Path:       c.FullPath(),
			StatusCode: status,
			IPAddress:  c.ClientIP(),
			CreatedAt:  time.Now(),
		}
		if actorID != nil {
			entry.ActorUserID = actorID
			var actor struct {
				Name  string
				Email string
			}
			if err := db.Table("users").
				Select("name, email").
				Where("id = ? AND business_id = ?", *actorID, *businessID).
				Take(&actor).Error; err == nil {
				entry.ActorName = actor.Name
				entry.ActorEmail = actor.Email
			}
		}
		if err := recordWithRetry(db, &entry); err != nil {
			log.Printf("audit: failed to record %s %s: %v", c.Request.Method, c.FullPath(), err)
		}
	}
}

// contextUUID reads a UUID string from the Gin request context.
func contextUUID(c *gin.Context, key string) *uuid.UUID {
	raw, exists := c.Get(key)
	if !exists {
		return nil
	}
	value, ok := raw.(string)
	if !ok {
		return nil
	}
	parsed, err := uuid.Parse(value)
	if err != nil {
		return nil
	}
	return &parsed
}

// pathEntityID resolves the entity id from route parameters.
func pathEntityID(c *gin.Context, entity string, businessID uuid.UUID) *uuid.UUID {
	if entity == "business" {
		return &businessID
	}
	raw := c.Param("id")
	if raw == "" {
		raw = c.Param("productId")
	}
	if raw == "" {
		return nil
	}
	parsed, err := uuid.Parse(raw)
	if err != nil {
		return nil
	}
	return &parsed
}

// snapshot reads a sanitized database row for before/after comparison.
func snapshot(db *gorm.DB, businessID uuid.UUID, entity string, id uuid.UUID) JSON {
	table, scoped := EntityTable(entity)
	if table == "" {
		return nil
	}
	query := "SELECT * FROM " + table + " WHERE id = ?"
	args := []any{id}
	if scoped {
		query += " AND business_id = ?"
		args = append(args, businessID)
	}
	rows, err := db.Raw(query, args...).Rows()
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return nil
	}
	columns, err := rows.Columns()
	if err != nil {
		return nil
	}
	values := make([]any, len(columns))
	pointers := make([]any, len(columns))
	for i := range values {
		pointers[i] = &values[i]
	}
	if err := rows.Scan(pointers...); err != nil {
		return nil
	}
	row := make(map[string]any, len(columns))
	for i, column := range columns {
		if bytes, ok := values[i].([]byte); ok {
			row[column] = string(bytes)
			continue
		}
		row[column] = values[i]
	}
	raw, err := json.Marshal(row)
	if err != nil {
		return nil
	}
	return Sanitize(JSON(raw))
}

// responseData extracts and sanitizes the success envelope data.
func responseData(body []byte) JSON {
	if len(body) == 0 {
		return nil
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || len(envelope.Data) == 0 {
		return nil
	}
	if string(envelope.Data) == "null" {
		return nil
	}
	return Sanitize(JSON(envelope.Data))
}

// recordWithRetry retries transient audit insert failures a few times.
func recordWithRetry(db *gorm.DB, entry *Entry) error {
	repo := NewRepository(db)
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if err = repo.Create(entry); err == nil {
			return nil
		}
		time.Sleep(time.Duration(attempt+1) * 10 * time.Millisecond)
	}
	return err
}

// recordTypeFromJSON reads the receipt record type from a commit response.
func recordTypeFromJSON(raw JSON) string {
	if raw.IsZero() {
		return ""
	}
	var object struct {
		RecordType string `json:"record_type"`
	}
	if err := json.Unmarshal(raw, &object); err != nil {
		return ""
	}
	return object.RecordType
}

// idFromJSON extracts a created entity id from a response payload.
func idFromJSON(raw JSON) *uuid.UUID {
	if raw.IsZero() {
		return nil
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil
	}
	for _, key := range []string{"id", "ID", "sale_id", "purchase_id"} {
		value, ok := object[key].(string)
		if !ok {
			continue
		}
		parsed, err := uuid.Parse(value)
		if err == nil {
			return &parsed
		}
	}
	return nil
}

type recordingWriter struct {
	gin.ResponseWriter
	body   bytes.Buffer
	status int
}

// WriteHeader records the status before delegating to Gin.
func (w *recordingWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Write buffers the response body while sending it to the client.
func (w *recordingWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	w.body.Write(data)
	return w.ResponseWriter.Write(data)
}

// Status returns the captured or underlying response status.
func (w *recordingWriter) Status() int {
	if w.status != 0 {
		return w.status
	}
	return w.ResponseWriter.Status()
}
