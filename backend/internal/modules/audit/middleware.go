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
		if entityID == nil {
			entityID = idFromJSON(after)
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
		if err := NewRepository(db).Create(&entry); err != nil {
			log.Printf("audit: failed to record %s %s: %v", c.Request.Method, c.FullPath(), err)
		}
	}
}

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

func (w *recordingWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *recordingWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	w.body.Write(data)
	return w.ResponseWriter.Write(data)
}

func (w *recordingWriter) Status() int {
	if w.status != 0 {
		return w.status
	}
	return w.ResponseWriter.Status()
}
