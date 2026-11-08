package permissions

import "testing"

func TestAllRolesAreValid(t *testing.T) {
	for _, role := range AllRoles {
		if !ValidRole(role) {
			t.Fatalf("ValidRole(%q) = false", role)
		}
	}
	if ValidRole("superuser") {
		t.Fatal("unknown roles must not be valid")
	}
}

func TestPermissionMatrix(t *testing.T) {
	tests := []struct {
		name       string
		role       string
		permission Permission
		want       bool
	}{
		{"owner reads audit", RoleOwner, AuditRead, true},
		{"admin reads audit", RoleAdmin, AuditRead, true},
		{"manager cannot read audit", RoleManager, AuditRead, false},
		{"accountant cannot read audit", RoleAccountant, AuditRead, false},
		{"owner manages team", RoleOwner, TeamManage, true},
		{"admin manages team", RoleAdmin, TeamManage, true},
		{"manager reads team", RoleManager, TeamRead, true},
		{"manager cannot manage team", RoleManager, TeamManage, false},
		{"cashier records sales", RoleCashier, SalesCreate, true},
		{"cashier records invoice payments", RoleCashier, InvoicePayment, true},
		{"cashier cannot read finance", RoleCashier, FinanceRead, false},
		{"cashier records customer payment", RoleCashier, CustomerPayment, true},
		{"accountant reads invoices", RoleAccountant, InvoiceRead, true},
		{"accountant records invoice payments", RoleAccountant, InvoicePayment, true},
		{"accountant reads finance", RoleAccountant, FinanceRead, true},
		{"accountant manages expenses", RoleAccountant, FinanceManage, true},
		{"accountant cannot delete finance", RoleAccountant, FinanceDelete, false},
		{"accountant cannot manage team", RoleAccountant, TeamManage, false},
		{"warehouse adjusts inventory", RoleWarehouse, InventoryAdjust, true},
		{"warehouse receives purchases", RoleWarehouse, PurchaseReceive, true},
		{"warehouse cannot create purchases", RoleWarehouse, PurchaseCreate, false},
		{"warehouse cannot record sales", RoleWarehouse, SalesCreate, false},
		{"warehouse cannot read invoices", RoleWarehouse, InvoiceRead, false},
		{"unknown role has no access", "superuser", SalesRead, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Allows(tt.role, tt.permission); got != tt.want {
				t.Fatalf("Allows(%q, %q) = %t, want %t", tt.role, tt.permission, got, tt.want)
			}
		})
	}
}

func TestForRoute(t *testing.T) {
	tests := []struct {
		method string
		path   string
		want   Permission
		mapped bool
	}{
		{"GET", "/api/v1/finance/summary", FinanceRead, true},
		{"POST", "/api/v1/purchases/:id/receive", PurchaseReceive, true},
		{"PATCH", "/api/v1/users/:id/role", TeamManage, true},
		{"DELETE", "/api/v1/products/:id", ProductDelete, true},
		{"GET", "/api/v1/notifications/unread-count", NotificationRead, true},
		{"POST", "/api/v1/sales", SalesCreate, true},
		{"GET", "/api/v1/not-a-route", "", false},
		{"POST", "/api/v1/finance/summary", "", false},
	}
	for _, tt := range tests {
		permission, ok := ForRoute(tt.method, tt.path)
		if ok != tt.mapped || permission != tt.want {
			t.Fatalf("ForRoute(%q, %q) = (%q, %t), want (%q, %t)", tt.method, tt.path, permission, ok, tt.want, tt.mapped)
		}
	}
}
