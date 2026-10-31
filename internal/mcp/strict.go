package mcp

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"sync"
)

// Strict mode (MCPX-33, CF-27): with mcp_strict on, the servers list has
// a warning row for each entry with keys termocode does not read, so a
// typo ("comand") or another app's key does not go unseen.

// extraKeys are the entry keys read outside the entry struct.
var extraKeys = []string{"registry", "bundle", "sandbox", "autostart", "gateway"}

var knownKeys = sync.OnceValue(func() map[string]bool {
	m := map[string]bool{}
	t := reflect.TypeOf(entry{})
	for i := 0; i < t.NumField(); i++ {
		if k, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ","); k != "" {
			m[k] = true
		}
	}
	for _, k := range extraKeys {
		m[k] = true
	}
	return m
})

// UnknownKeys lists the entry's keys termocode does not read, sorted;
// keys that start with "_" or "$" are comments.
func (s Server) UnknownKeys() []string {
	var m map[string]json.RawMessage
	if json.Unmarshal(s.raw, &m) != nil {
		return nil
	}
	var out []string
	for k := range m {
		if !knownKeys()[k] && !strings.HasPrefix(k, "_") && !strings.HasPrefix(k, "$") {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
