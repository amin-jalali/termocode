package app

import (
	"strings"
	"testing"
)

// KB-3: one table says what each shared key means in Databases and in
// Containers. The two ? cards must agree with it, and docgen prints it.

func keyCardKeys(rows ...[][2]string) map[string]bool {
	keys := map[string]bool{}
	for _, set := range rows {
		for _, r := range set {
			for _, k := range strings.Split(r[0], " / ") {
				keys[strings.TrimSpace(k)] = true
			}
		}
	}
	return keys
}

func TestKB3KeyTableMatchesTheKeyCards(t *testing.T) {
	var db [][2]string
	for _, r := range dbTreeKeyRows() {
		db = append(db, [2]string{r.Key, r.What})
	}
	var ct [][2]string
	for _, r := range []svcRow{
		{kind: svcRowHostItem, host: hostContainer},
		{kind: svcRowGroup, section: "grp/containers/x"},
		{kind: svcRowHostItem, host: hostImage},
		{kind: svcRowHostItem, host: hostVolume},
		{kind: svcRowHostItem, host: hostContext},
		{kind: svcRowHostItem, host: hostRegistryConnect},
		{kind: svcRowSection, section: "host/volumes"},
		{kind: svcRowSection, section: "host/registries"},
		{kind: svcRowSection, section: svcSecCompose},
		{kind: svcRowService},
	} {
		_, rows := hostKeysHelp(r)
		for _, kr := range rows {
			ct = append(ct, [2]string{kr.Key, kr.What})
		}
	}
	dbKeys, ctKeys := keyCardKeys(db), keyCardKeys(ct)
	table := PanelKeyCatalog()
	if len(table) < 8 {
		t.Fatalf("the key table has %d rows", len(table))
	}
	seen := map[string]bool{}
	for _, row := range table {
		if seen[row.Key] {
			t.Errorf("key %q is in the table twice", row.Key)
		}
		seen[row.Key] = true
		if row.Role == "" || (row.Databases == "" && row.Containers == "") {
			t.Errorf("row %q is empty: %+v", row.Key, row)
		}
		if row.Databases != "" && !dbKeys[row.Key] {
			t.Errorf("table says %s means %q in Databases, but the ? card has no such key", row.Key, row.Databases)
		}
		if row.Containers != "" && !ctKeys[row.Key] {
			t.Errorf("table says %s means %q in Containers, but the ? cards have no such key", row.Key, row.Containers)
		}
	}
	// The decisions of KB-1 and KB-2.
	for key, role := range map[string]string{"D": "remove", "$": "shell", "s": "start", "x": "stop"} {
		var got string
		for _, row := range table {
			if row.Key == key {
				got = row.Role
			}
		}
		if !strings.Contains(strings.ToLower(got), role) {
			t.Errorf("key %s: role %q, want %q", key, got, role)
		}
	}
}
