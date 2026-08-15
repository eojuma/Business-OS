package migrations

import "testing"

func TestLoadMigrations(t *testing.T) {
	migrations, err := load()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if len(migrations) == 0 {
		t.Fatal("expected at least one migration")
	}
	for i, migration := range migrations {
		if migration.up == "" || migration.down == "" {
			t.Fatalf("migration %s has an empty section", migration.name)
		}
		if i > 0 && migrations[i-1].version >= migration.version {
			t.Fatalf("migrations are not ordered by unique version")
		}
	}
}
