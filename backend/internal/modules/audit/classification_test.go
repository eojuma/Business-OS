package audit

import (
	"encoding/json"
	"testing"
)

const testEntityProduct = "product"

func TestClassify(t *testing.T) {
	tests := []struct {
		method, path string
		action       string
		entity       string
		ok           bool
	}{
		{"POST", "/api/v1/products", ActionCreate, testEntityProduct, true},
		{"PATCH", "/api/v1/products/:id", ActionUpdate, testEntityProduct, true},
		{"DELETE", "/api/v1/products/:id", ActionDelete, testEntityProduct, true},
		{"POST", "/api/v1/customers/:id/payments", ActionPayment, "customer", true},
		{"POST", "/api/v1/suppliers/:id/restore", ActionRestore, "supplier", true},
		{"PATCH", "/api/v1/users/:id/role", ActionRoleChange, "user", true},
		{"POST", "/api/v1/purchases/:id/receive", ActionReceive, "purchase", true},
		{"POST", "/api/v1/receipts/commit", ActionReceiptCommit, "receipt", true},
		{"POST", "/api/v1/assistant/confirm", ActionAssistantCommit, "sale", true},
		{"POST", "/api/v1/inventory/movements", ActionStockMovement, "stock_movement", true},
		{"POST", "/api/v1/finance/expenses", ActionCreate, "expense", true},
		{"PATCH", "/api/v1/business", ActionUpdate, "business", true},
		{"GET", "/api/v1/products", "", "", false},
		{"POST", "/api/v1/auth/login", "", "", false},
		{"POST", "/api/v1/receipts/scan", "", "", false},
		{"POST", "/api/v1/notifications/read-all", "", "", false},
		{"POST", "/api/v1/unknown", "", "", false},
	}
	for _, tt := range tests {
		action, entity, ok := Classify(tt.method, tt.path)
		if action != tt.action || entity != tt.entity || ok != tt.ok {
			t.Fatalf("Classify(%q, %q) = (%q, %q, %t), want (%q, %q, %t)",
				tt.method, tt.path, action, entity, ok, tt.action, tt.entity, tt.ok)
		}
	}
}

func TestSanitizeRemovesSecretsRecursively(t *testing.T) {
	raw := JSON(`{
		"name":"Ada",
		"password":"hunter2",
		"password_hash":"$2a$10$hash",
		"access_token":"abc",
		"nested":{"api_key":"xyz","safe":"kept"},
		"items":[{"secret":"s","label":"ok"}]
	}`)
	cleaned := Sanitize(raw)

	var value map[string]any
	if err := json.Unmarshal(cleaned, &value); err != nil {
		t.Fatalf("decode cleaned JSON: %v", err)
	}
	if value["name"] != "Ada" {
		t.Fatalf("name = %v", value["name"])
	}
	for _, key := range []string{"password", "password_hash", "access_token"} {
		if _, exists := value[key]; exists {
			t.Fatalf("sensitive key %q was not removed", key)
		}
	}
	nested, ok := value["nested"].(map[string]any)
	if !ok {
		t.Fatalf("nested value = %T", value["nested"])
	}
	if _, exists := nested["api_key"]; exists {
		t.Fatal("nested api_key was not removed")
	}
	if nested["safe"] != "kept" {
		t.Fatalf("nested safe value = %v", nested["safe"])
	}
	items, ok := value["items"].([]any)
	if !ok || len(items) == 0 {
		t.Fatalf("items value = %T", value["items"])
	}
	item, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("item value = %T", items[0])
	}
	if _, exists := item["secret"]; exists {
		t.Fatal("array secret was not removed")
	}
	if item["label"] != "ok" {
		t.Fatalf("array label = %v", item["label"])
	}
}

func TestSanitizeHandlesInvalidJSON(t *testing.T) {
	if got := Sanitize(JSON(`{not-json`)); !got.IsZero() {
		t.Fatalf("expected invalid JSON to sanitize to nil, got %s", got)
	}
	if got := Sanitize(nil); !got.IsZero() {
		t.Fatal("expected nil JSON to stay nil")
	}
}
