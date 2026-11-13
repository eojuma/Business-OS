package audit

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestRepositoryListFiltersAndPagination documents its scenario and asserts the expected audit behaviour.
func TestRepositoryListFiltersAndPagination(t *testing.T) {
	db, businessID, userID := setupAuditTestDB(t)
	repo := NewRepository(db)
	now := time.Now().UTC()

	entries := []*Entry{
		{BusinessID: businessID, ActorUserID: &userID, Action: ActionCreate, EntityType: testEntityProduct, CreatedAt: now.Add(-3 * time.Hour)},
		{BusinessID: businessID, ActorUserID: &userID, Action: ActionPayment, EntityType: "invoice", CreatedAt: now.Add(-2 * time.Hour)},
		{BusinessID: businessID, Action: ActionPayment, EntityType: "customer", CreatedAt: now.Add(-time.Hour)},
	}
	for _, entry := range entries {
		if err := repo.Create(entry); err != nil {
			t.Fatalf("create entry: %v", err)
		}
	}

	actionEntries, total, err := repo.List(businessID, ListFilter{Page: 1, Limit: 20, Action: ActionPayment})
	if err != nil {
		t.Fatalf("list by action: %v", err)
	}
	if total != 2 || len(actionEntries) != 2 {
		t.Fatalf("action filter total=%d len=%d", total, len(actionEntries))
	}

	actorEntries, total, err := repo.List(businessID, ListFilter{Page: 1, Limit: 20, ActorID: &userID})
	if err != nil {
		t.Fatalf("list by actor: %v", err)
	}
	if total != 2 || len(actorEntries) != 2 {
		t.Fatalf("actor filter total=%d len=%d", total, len(actorEntries))
	}

	entityEntries, total, err := repo.List(businessID, ListFilter{Page: 1, Limit: 20, EntityType: "invoice"})
	if err != nil {
		t.Fatalf("list by entity: %v", err)
	}
	if total != 1 || len(entityEntries) != 1 || entityEntries[0].EntityType != "invoice" {
		t.Fatalf("entity filter total=%d len=%d", total, len(entityEntries))
	}

	from := now.Add(-150 * time.Minute)
	dateEntries, total, err := repo.List(businessID, ListFilter{Page: 1, Limit: 20, DateFrom: &from})
	if err != nil {
		t.Fatalf("list by date: %v", err)
	}
	if total != 2 || len(dateEntries) != 2 {
		t.Fatalf("date filter total=%d len=%d", total, len(dateEntries))
	}

	firstPage, total, err := repo.List(businessID, ListFilter{Page: 1, Limit: 2})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if total != 3 || len(firstPage) != 2 {
		t.Fatalf("page 1 total=%d len=%d", total, len(firstPage))
	}
	secondPage, _, err := repo.List(businessID, ListFilter{Page: 2, Limit: 2})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(secondPage) != 1 {
		t.Fatalf("page 2 len=%d", len(secondPage))
	}
	if firstPage[0].ID == secondPage[0].ID {
		t.Fatal("pagination returned duplicate rows")
	}

	otherEntries, total, err := repo.List(uuid.New(), ListFilter{Page: 1, Limit: 20})
	if err != nil {
		t.Fatalf("other business: %v", err)
	}
	if total != 0 || len(otherEntries) != 0 {
		t.Fatalf("other business total=%d len=%d", total, len(otherEntries))
	}
}
