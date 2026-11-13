package audit

import (
	"strings"
)

// Action and entity labels stored on audit entries.
const (
	ActionCreate          = "create"
	ActionUpdate          = "update"
	ActionDelete          = "delete"
	ActionPayment         = "payment"
	ActionRestore         = "restore"
	ActionReceive         = "receive"
	ActionRoleChange      = "role_change"
	ActionReceiptCommit   = "receipt_commit"
	ActionAssistantCommit = "assistant_commit"
	ActionStockMovement   = "stock_movement"
)

var entityPrefixes = []struct {
	prefix string
	entity string
}{
	{"/api/v1/finance/expenses", "expense"},
	{"/api/v1/inventory/movements", "stock_movement"},
	{"/api/v1/receipts/commit", "receipt"},
	{"/api/v1/assistant/confirm", "sale"},
	{"/api/v1/products", "product"},
	{"/api/v1/customers", "customer"},
	{"/api/v1/suppliers", "supplier"},
	{"/api/v1/sales", "sale"},
	{"/api/v1/purchases", "purchase"},
	{"/api/v1/invoices", "invoice"},
	{"/api/v1/business", "business"},
	{"/api/v1/users", "user"},
}

// Classify maps a mutating route to an audit action and entity type.
func Classify(method, fullPath string) (action, entity string, ok bool) {
	switch method {
	case "POST", "PATCH", "PUT", "DELETE":
	default:
		return "", "", false
	}

	// Personal auth changes can contain credentials; notifications are routine
	// state updates. Neither is part of the business activity trail.
	if strings.HasPrefix(fullPath, "/api/v1/auth/") || strings.HasPrefix(fullPath, "/api/v1/notifications") {
		return "", "", false
	}
	if fullPath == "/api/v1/receipts/scan" {
		return "", "", false
	}

	for _, candidate := range entityPrefixes {
		if strings.HasPrefix(fullPath, candidate.prefix) {
			entity = candidate.entity
			break
		}
	}
	if entity == "" {
		return "", "", false
	}

	switch {
	case strings.HasSuffix(fullPath, "/payments"):
		action = ActionPayment
	case strings.HasSuffix(fullPath, "/restore"):
		action = ActionRestore
	case strings.HasSuffix(fullPath, "/receive"):
		action = ActionReceive
	case strings.HasSuffix(fullPath, "/role"):
		action = ActionRoleChange
	case fullPath == "/api/v1/receipts/commit":
		action = ActionReceiptCommit
	case fullPath == "/api/v1/assistant/confirm":
		action = ActionAssistantCommit
	case fullPath == "/api/v1/inventory/movements":
		action = ActionStockMovement
	case method == "POST":
		action = ActionCreate
	case method == "PATCH" || method == "PUT":
		action = ActionUpdate
	case method == "DELETE":
		action = ActionDelete
	}
	if action == "" {
		return "", "", false
	}
	return action, entity, true
}

// EntityTable returns the table a pre-change snapshot should read, and whether
// the table is scoped by business_id.
func EntityTable(entity string) (string, bool) {
	switch entity {
	case "product":
		return "products", true
	case "customer":
		return "customers", true
	case "supplier":
		return "suppliers", true
	case "sale":
		return "sales", true
	case "purchase":
		return "purchases", true
	case "expense":
		return "expenses", true
	case "invoice":
		return "invoices", true
	case "user":
		return "users", true
	case "business":
		return "businesses", false
	default:
		return "", false
	}
}

// SensitiveKey reports whether a snapshot key must never be persisted.
func SensitiveKey(key string) bool {
	key = strings.ToLower(key)
	for _, fragment := range []string{
		"password", "token", "secret", "credential", "authorization",
		"api_key", "apikey", "private_key", "google_id",
	} {
		if strings.Contains(key, fragment) {
			return true
		}
	}
	return false
}
