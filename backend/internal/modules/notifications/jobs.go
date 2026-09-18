package notifications

import (
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Generator scans business data and persists notifications for conditions
// that need attention. It is meant to run on a schedule, not on request.
type Generator struct {
	repo Repository
	db   *gorm.DB
}

func NewGenerator(db *gorm.DB) *Generator {
	return &Generator{repo: NewRepository(db), db: db}
}

type lowStockRow struct {
	BusinessID  uuid.UUID
	ProductID   uuid.UUID
	ProductName string
	Unit        string
	Quantity    int64
	Threshold   int64
	Recipient   string
}

type creditRow struct {
	BusinessID   uuid.UUID
	CustomerID   uuid.UUID
	CustomerName string
	Balance      int64
	CreditLimit  int64
	Recipient    string
}

func formatCents(cents int64) string {
	return fmt.Sprintf("KSh %.2f", float64(cents)/100)
}

// GenerateLowStock creates one unread alert per product that is at or below
// its low-stock threshold. Existing unread alerts for the same product are
// left alone so the scheduler does not spam duplicates.
func (g *Generator) GenerateLowStock() (int, error) {
	var rows []lowStockRow
	err := g.db.Raw(`
		SELECT sl.business_id,
		       sl.product_id,
		       p.name AS product_name,
		       p.unit,
		       sl.quantity,
		       sl.low_stock_threshold AS threshold,
		       COALESCE((SELECT u.email FROM users u
		                 WHERE u.business_id = sl.business_id AND u.role = 'owner'
		                 ORDER BY u.created_at ASC LIMIT 1), '') AS recipient
		FROM stock_levels sl
		JOIN products p ON p.id = sl.product_id
		WHERE sl.quantity <= sl.low_stock_threshold
	`).Scan(&rows).Error
	if err != nil {
		return 0, err
	}

	created := 0
	for _, row := range rows {
		exists, err := g.repo.ExistsUnread(row.BusinessID, TypeLowStock, row.ProductID)
		if err != nil {
			return created, err
		}
		if exists {
			continue
		}

		severity := SeverityWarning
		if row.Quantity <= 0 {
			severity = SeverityCritical
		}

		entityID := row.ProductID
		n := &Notification{
			BusinessID: row.BusinessID,
			Type:       TypeLowStock,
			Severity:   severity,
			Recipient:  row.Recipient,
			Title:      "Low stock: " + row.ProductName,
			Message:    fmt.Sprintf("%s has %d %s remaining (threshold %d).", row.ProductName, row.Quantity, row.Unit, row.Threshold),
			EntityID:   &entityID,
			EntityName: row.ProductName,
		}
		if err := g.repo.Create(n); err != nil {
			return created, err
		}
		created++
	}
	return created, nil
}

// GenerateCreditAlerts creates one unread alert per customer who has used at
// least 80% of their credit limit. Reaching the limit is critical.
func (g *Generator) GenerateCreditAlerts() (int, error) {
	var rows []creditRow
	err := g.db.Raw(`
		SELECT c.business_id,
		       c.id AS customer_id,
		       c.name AS customer_name,
		       c.balance,
		       c.credit_limit,
		       COALESCE((SELECT u.email FROM users u
		                 WHERE u.business_id = c.business_id AND u.role = 'owner'
		                 ORDER BY u.created_at ASC LIMIT 1), '') AS recipient
		FROM customers c
		WHERE c.credit_limit > 0 AND c.balance >= (c.credit_limit * 80 / 100)
	`).Scan(&rows).Error
	if err != nil {
		return 0, err
	}

	created := 0
	for _, row := range rows {
		exists, err := g.repo.ExistsUnread(row.BusinessID, TypeCreditLimit, row.CustomerID)
		if err != nil {
			return created, err
		}
		if exists {
			continue
		}

		severity := SeverityWarning
		if row.Balance >= row.CreditLimit {
			severity = SeverityCritical
		}

		entityID := row.CustomerID
		n := &Notification{
			BusinessID: row.BusinessID,
			Type:       TypeCreditLimit,
			Severity:   severity,
			Recipient:  row.Recipient,
			Title:      "Credit limit: " + row.CustomerName,
			Message:    fmt.Sprintf("%s owes %s against a %s credit limit.", row.CustomerName, formatCents(row.Balance), formatCents(row.CreditLimit)),
			EntityID:   &entityID,
			EntityName: row.CustomerName,
		}
		if err := g.repo.Create(n); err != nil {
			return created, err
		}
		created++
	}
	return created, nil
}

// NotifyProductLowStock is the real-time path. It is called right after a
// stock movement (a sale or an adjustment) so the alert appears on the
// notifications page immediately instead of waiting for the next sweep.
// When stock is replenished above the threshold, any stale unread low-stock
// alert for that product is cleared.
func (g *Generator) NotifyProductLowStock(businessID, productID uuid.UUID) error {
	var row lowStockRow
	err := g.db.Raw(`
		SELECT sl.business_id,
		       sl.product_id,
		       p.name AS product_name,
		       p.unit,
		       sl.quantity,
		       sl.low_stock_threshold AS threshold,
		       COALESCE((SELECT u.email FROM users u
		                 WHERE u.business_id = sl.business_id AND u.role = 'owner'
		                 ORDER BY u.created_at ASC LIMIT 1), '') AS recipient
		FROM stock_levels sl
		JOIN products p ON p.id = sl.product_id
		WHERE sl.business_id = ? AND sl.product_id = ?
	`, businessID, productID).Scan(&row).Error
	if err != nil {
		return err
	}
	if row.ProductID == uuid.Nil {
		return nil
	}

	if row.Quantity > row.Threshold {
		return g.repo.MarkReadByEntity(businessID, TypeLowStock, productID)
	}

	exists, err := g.repo.ExistsUnread(businessID, TypeLowStock, productID)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	severity := SeverityWarning
	if row.Quantity <= 0 {
		severity = SeverityCritical
	}

	entityID := row.ProductID
	return g.repo.Create(&Notification{
		BusinessID: row.BusinessID,
		Type:       TypeLowStock,
		Severity:   severity,
		Recipient:  row.Recipient,
		Title:      "Low stock: " + row.ProductName,
		Message:    fmt.Sprintf("%s has %d %s remaining (threshold %d).", row.ProductName, row.Quantity, row.Unit, row.Threshold),
		EntityID:   &entityID,
		EntityName: row.ProductName,
	})
}

// Run executes every generation pass once and logs a summary.
func (g *Generator) Run() {
	if n, err := g.GenerateLowStock(); err != nil {
		log.Printf("notifications: low-stock generation failed: %v", err)
	} else if n > 0 {
		log.Printf("notifications: generated %d low-stock alert(s)", n)
	}

	if n, err := g.GenerateCreditAlerts(); err != nil {
		log.Printf("notifications: credit-limit generation failed: %v", err)
	} else if n > 0 {
		log.Printf("notifications: generated %d credit-limit alert(s)", n)
	}
}

// StartScheduler runs generation immediately, then on every tick for the
// lifetime of the process. Intended to be called from main as a goroutine.
func StartScheduler(db *gorm.DB, interval time.Duration) {
	g := NewGenerator(db)
	go func() {
		g.Run()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			g.Run()
		}
	}()
}
