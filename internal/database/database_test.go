package database

import (
	"strings"
	"testing"
)

func TestLoadMigrationsAreOrderedAndUnique(t *testing.T) {
	items, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations() failed: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("no migrations were embedded")
	}
	seen := map[int]bool{}
	previous := 0
	for _, item := range items {
		if item.version <= previous {
			t.Fatalf("migration %s is out of order after version %d", item.name, previous)
		}
		if seen[item.version] {
			t.Fatalf("duplicate migration version %d", item.version)
		}
		if strings.TrimSpace(item.body) == "" {
			t.Fatalf("migration %s is empty", item.name)
		}
		seen[item.version] = true
		previous = item.version
	}
	if got := ExpectedSchemaVersion(); got != previous {
		t.Fatalf("ExpectedSchemaVersion() = %d, want %d", got, previous)
	}
}

// The baseline predates per-file versioning and already contained the rows that
// recorded versions 3 and 4, so it must keep them to stay compatible with
// databases created by earlier releases.
func TestBaselineKeepsLegacyVersionRows(t *testing.T) {
	items, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations() failed: %v", err)
	}
	for _, item := range items {
		if !strings.Contains(item.name, "baseline") {
			continue
		}
		for _, version := range []string{"(3)", "(4)"} {
			if !strings.Contains(item.body, "INSERT INTO schema_migrations(version) VALUES "+version) {
				t.Fatalf("baseline no longer records legacy version %s", version)
			}
		}
		return
	}
	t.Fatal("baseline migration is missing")
}
