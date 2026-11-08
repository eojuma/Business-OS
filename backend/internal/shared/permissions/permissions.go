// Package permissions defines the role model and the permission matrix used by
// the API. Roles are carried in the JWT, and every protected route resolves to
// one permission so enforcement stays consistent across modules.
package permissions

// Role names stored on users and embedded in the JWT `role` claim.
const (
	RoleOwner      = "owner"
	RoleAdmin      = "admin"
	RoleManager    = "manager"
	RoleCashier    = "cashier"
	RoleAccountant = "accountant"
	RoleWarehouse  = "warehouse"
)

// Permission is a single capability checked at the route boundary.
type Permission string

// Module actions. Read and write capabilities are separate so a role can be
// granted visibility without allowing changes.
const (
	AccountSelf Permission = "account:self"

	BusinessRead   Permission = "business:read"
	BusinessManage Permission = "business:manage"

	TeamRead   Permission = "team:read"
	TeamManage Permission = "team:manage"

	SalesRead   Permission = "sales:read"
	SalesCreate Permission = "sales:create"

	InvoiceRead    Permission = "invoices:read"
	InvoicePayment Permission = "invoices:payment"

	PurchaseRead    Permission = "purchases:read"
	PurchaseCreate  Permission = "purchases:create"
	PurchaseReceive Permission = "purchases:receive"
	PurchaseDelete  Permission = "purchases:delete"

	ProductRead   Permission = "products:read"
	ProductManage Permission = "products:manage"
	ProductDelete Permission = "products:delete"

	InventoryRead   Permission = "inventory:read"
	InventoryAdjust Permission = "inventory:adjust"

	CustomerRead    Permission = "customers:read"
	CustomerManage  Permission = "customers:manage"
	CustomerPayment Permission = "customers:payment"

	SupplierRead    Permission = "suppliers:read"
	SupplierManage  Permission = "suppliers:manage"
	SupplierPayment Permission = "suppliers:payment"
	SupplierDelete  Permission = "suppliers:delete"

	FinanceRead   Permission = "finance:read"
	FinanceManage Permission = "finance:manage"
	FinanceDelete Permission = "finance:delete"

	ReportRead    Permission = "reports:read"
	AnalyticsRead Permission = "analytics:read"

	NotificationRead  Permission = "notifications:read"
	NotificationWrite Permission = "notifications:write"

	AssistantUse Permission = "assistant:use"
	ReceiptUse   Permission = "receipts:use"

	AuditRead Permission = "audit:read"
)

// AllRoles lists every supported role in display order.
var AllRoles = []string{RoleOwner, RoleAdmin, RoleManager, RoleCashier, RoleAccountant, RoleWarehouse}

var allPermissions = []Permission{
	AccountSelf,
	BusinessRead, BusinessManage,
	TeamRead, TeamManage,
	SalesRead, SalesCreate,
	InvoiceRead, InvoicePayment,
	PurchaseRead, PurchaseCreate, PurchaseReceive, PurchaseDelete,
	ProductRead, ProductManage, ProductDelete,
	InventoryRead, InventoryAdjust,
	CustomerRead, CustomerManage, CustomerPayment,
	SupplierRead, SupplierManage, SupplierPayment, SupplierDelete,
	FinanceRead, FinanceManage, FinanceDelete,
	ReportRead, AnalyticsRead,
	NotificationRead, NotificationWrite,
	AssistantUse, ReceiptUse,
	AuditRead,
}

var matrix = map[string]map[Permission]bool{
	RoleOwner: grant(allPermissions),
	RoleAdmin: grant(allPermissions),
	RoleManager: grant([]Permission{
		AccountSelf,
		BusinessRead,
		TeamRead,
		SalesRead, SalesCreate,
		InvoiceRead, InvoicePayment,
		PurchaseRead, PurchaseCreate, PurchaseReceive, PurchaseDelete,
		ProductRead, ProductManage, ProductDelete,
		InventoryRead, InventoryAdjust,
		CustomerRead, CustomerManage, CustomerPayment,
		SupplierRead, SupplierManage, SupplierPayment, SupplierDelete,
		FinanceRead, FinanceManage, FinanceDelete,
		ReportRead, AnalyticsRead,
		NotificationRead, NotificationWrite,
		AssistantUse, ReceiptUse,
	}),
	RoleCashier: grant([]Permission{
		AccountSelf,
		BusinessRead,
		SalesRead, SalesCreate,
		InvoiceRead, InvoicePayment,
		ProductRead,
		InventoryRead,
		CustomerRead, CustomerManage, CustomerPayment,
		SupplierRead,
		NotificationRead, NotificationWrite,
		AssistantUse, ReceiptUse,
	}),
	RoleAccountant: grant([]Permission{
		AccountSelf,
		BusinessRead,
		TeamRead,
		SalesRead,
		InvoiceRead, InvoicePayment,
		PurchaseRead,
		ProductRead,
		InventoryRead,
		CustomerRead, CustomerPayment,
		SupplierRead, SupplierPayment,
		FinanceRead, FinanceManage,
		ReportRead, AnalyticsRead,
		NotificationRead, NotificationWrite,
		AssistantUse,
	}),
	RoleWarehouse: grant([]Permission{
		AccountSelf,
		BusinessRead,
		PurchaseRead, PurchaseReceive,
		ProductRead,
		InventoryRead, InventoryAdjust,
		SupplierRead,
		NotificationRead, NotificationWrite,
		AssistantUse,
	}),
}

// grant converts a permission slice into the lookup used by Allows.
func grant(permissions []Permission) map[Permission]bool {
	granted := make(map[Permission]bool, len(permissions))
	for _, permission := range permissions {
		granted[permission] = true
	}
	return granted
}

// ValidRole reports whether role is one the API can issue and enforce.
func ValidRole(role string) bool {
	_, ok := matrix[role]
	return ok
}

// Allows reports whether role may perform permission.
func Allows(role string, permission Permission) bool {
	return matrix[role][permission]
}

// RoleLabel returns a human-readable name for API responses and docs.
func RoleLabel(role string) string {
	switch role {
	case RoleOwner:
		return "Owner"
	case RoleAdmin:
		return "Admin"
	case RoleManager:
		return "Manager"
	case RoleCashier:
		return "Sales / Cashier"
	case RoleAccountant:
		return "Accountant"
	case RoleWarehouse:
		return "Warehouse"
	default:
		return role
	}
}
