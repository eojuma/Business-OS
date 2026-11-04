package auth

import (
	"testing"

	"github.com/google/uuid"
)

const testRoleCashier = "cashier"

func TestUpdateTeamMemberRole(t *testing.T) {
	businessID := uuid.New()
	user := &User{ID: uuid.New(), BusinessID: businessID, Name: "Member", Email: "member@example.com", Role: testRoleCashier}
	repo := &fakeRepository{users: []*User{user}}
	svc := testService(repo, nil, nil)

	updated, err := svc.UpdateTeamMemberRole(businessID, user.ID, "accountant")
	if err != nil {
		t.Fatalf("UpdateTeamMemberRole failed: %v", err)
	}
	if updated.Role != "accountant" {
		t.Fatalf("returned role = %q, want accountant", updated.Role)
	}
	if user.Role != "accountant" {
		t.Fatalf("stored role = %q, want accountant", user.Role)
	}
}

func TestUpdateTeamMemberRoleRejectsInvalidRole(t *testing.T) {
	businessID := uuid.New()
	user := &User{ID: uuid.New(), BusinessID: businessID, Role: testRoleCashier}
	svc := testService(&fakeRepository{users: []*User{user}}, nil, nil)

	if _, err := svc.UpdateTeamMemberRole(businessID, user.ID, "superuser"); err != ErrInvalidRole {
		t.Fatalf("error = %v, want %v", err, ErrInvalidRole)
	}
	if user.Role != testRoleCashier {
		t.Fatalf("invalid role must not change the stored role, got %q", user.Role)
	}
}

func TestUpdateTeamMemberRoleIsScopedToBusiness(t *testing.T) {
	user := &User{ID: uuid.New(), BusinessID: uuid.New(), Role: testRoleCashier}
	svc := testService(&fakeRepository{users: []*User{user}}, nil, nil)

	if _, err := svc.UpdateTeamMemberRole(uuid.New(), user.ID, "manager"); err != ErrTeamMemberNotFound {
		t.Fatalf("error = %v, want %v", err, ErrTeamMemberNotFound)
	}
	if user.Role != testRoleCashier {
		t.Fatalf("cross-business update changed role to %q", user.Role)
	}
}
