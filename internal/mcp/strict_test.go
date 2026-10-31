package mcp

import (
	"reflect"
	"testing"
)

// Spec (MCPX-33, CF-27): the keys of an entry termocode does not read
// (a typo, another app's key) are listed, sorted; "_" and "$" keys are
// comments and do not count.
func TestServerUnknownKeys(t *testing.T) {
	body := `{"mcpServers": {
		"a": {"command": "x", "comand": "y", "envv": {}, "_comment": "c", "$schema": "s",
		      "sandbox": true, "registry": {"name": "n", "version": "1"}, "autostart": false, "gateway": true, "bundle": {}},
		"b": {"url": "https://example.com/mcp", "headers": {}, "oauth": {}, "auth": {}, "readOnly": true}}}`
	list, _, err := ParseFile([]byte(body), SourceUser, "/x/mcp.json")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]string{}
	for _, s := range list {
		got[s.Name] = s.UnknownKeys()
	}
	if !reflect.DeepEqual(got["a"], []string{"comand", "envv"}) || len(got["b"]) != 0 {
		t.Fatalf("unknown keys %v", got)
	}
}

// The servers list status carries them.
func TestStatusUnknownKeys(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"fake": map[string]any{"command": "x", "comand": "y"}})
	m := e.manager(t, 0, nil)
	defer m.Close()
	for _, st := range m.Servers() {
		if st.Name == "fake" && reflect.DeepEqual(st.UnknownKeys, []string{"comand"}) {
			return
		}
	}
	t.Fatalf("status %+v", m.Servers())
}
