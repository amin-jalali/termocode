package migrate_test

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/migrate"
)

// Every setting an import writes is a real termocode setting. (Moved from
// TestCatalogCoverage in plan_test.go: the app imports migrate, so only
// an external test package may import the app.)
func TestSettingTargetsAreInTheSettingsCatalog(t *testing.T) {
	settings := map[string]bool{}
	for _, entry := range app.SettingsCatalog() {
		if entry.Key != "" {
			settings[entry.Key] = true
		}
	}
	for source, table := range migrate.SettingMaps {
		for _, target := range table {
			if !settings[target] {
				t.Errorf("%s setting target %q is absent from SettingsCatalog", source, target)
			}
		}
	}
}
