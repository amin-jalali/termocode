package mcp

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Versions (MCPX-32): a server added from the catalog is pinned (its
// "registry" key and the version in its args or URL). With the catalog
// list termocode tells when a newer version or a deprecated flag is
// out; Update moves the pin and keeps everything the user changed.

// Auto-update modes (mcp_auto_update).
const (
	AutoUpdateOff    = "off"    // never checks by itself
	AutoUpdateNotify = "notify" // checks once a day when the servers list opens, tells
	AutoUpdateOn     = "on"     // the same, and updates the user's pinned servers
)

// ParseAutoUpdate reads mcp_auto_update; unknown values are off.
func ParseAutoUpdate(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case AutoUpdateNotify:
		return AutoUpdateNotify
	case AutoUpdateOn:
		return AutoUpdateOn
	}
	return AutoUpdateOff
}

// RegistryRef is an entry's "registry" key.
type RegistryRef struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	URL     string `json:"url,omitempty"` // a custom catalog; "" the official one
}

// RegistryOf reads s's "registry" key.
func RegistryOf(s Server) (RegistryRef, bool) {
	var e struct {
		Registry *RegistryRef `json:"registry"`
	}
	if json.Unmarshal(s.Raw(), &e) != nil || e.Registry == nil || e.Registry.Name == "" {
		return RegistryRef{}, false
	}
	return *e.Registry, true
}

// Update is what the catalog says about one installed server.
type Update struct {
	Server     string
	Current    string
	Latest     string
	Deprecated bool
	Status     string
	Entry      CatalogEntry
}

// Newer reports a catalog version above the pinned one.
func (u Update) Newer() bool { return u.Latest != "" && compareVersions(u.Latest, u.Current) > 0 }

// FindUpdates matches the servers that came from the catalog against
// its list, by server name.
func FindUpdates(servers []Server, catalog []CatalogEntry) map[string]Update {
	by := map[string]CatalogEntry{}
	for _, e := range catalog {
		by[e.Name] = e
	}
	out := map[string]Update{}
	for _, s := range servers {
		ref, ok := RegistryOf(s)
		if !ok {
			continue
		}
		e, ok := by[ref.Name]
		if !ok {
			continue
		}
		u := Update{Server: s.Name, Current: ref.Version, Latest: e.Version, Deprecated: e.Deprecated(), Status: e.Status, Entry: e}
		if u.Newer() || u.Deprecated {
			out[s.Name] = u
		}
	}
	return out
}

// ApplyUpdate pins server name in path to e's version: command, args,
// url and the registry key change; env, headers and every other key the
// user set stay.
func ApplyUpdate(path, name string, e CatalogEntry) error {
	_, fresh, err := e.ServerEntry()
	if err != nil {
		return err
	}
	return editServers(path, func(servers map[string]any) error {
		cur, _ := servers[name].(map[string]any)
		if cur == nil {
			cur = map[string]any{}
		}
		for _, k := range []string{"command", "args", "url", "type", "registry"} {
			delete(cur, k)
			if v, ok := fresh[k]; ok {
				cur[k] = v
			}
		}
		for _, k := range []string{"env", "headers"} {
			nv, _ := fresh[k].(map[string]any)
			old, _ := cur[k].(map[string]any)
			if nv == nil {
				continue
			}
			if old == nil {
				old = map[string]any{}
			}
			for key, v := range nv {
				if _, ok := old[key]; !ok {
					old[key] = v // a new variable; the user's values stay
				}
			}
			cur[k] = old
		}
		servers[name] = cur
		return nil
	})
}

// compareVersions compares dotted versions number by number (1.10 >
// 1.9); a part that is not a number compares as text.
func compareVersions(a, b string) int {
	pa := strings.Split(strings.TrimPrefix(a, "v"), ".")
	pb := strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y string
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		nx, ex := strconv.Atoi(x)
		ny, ey := strconv.Atoi(y)
		switch {
		case ex == nil && ey == nil:
			if nx != ny {
				if nx > ny {
					return 1
				}
				return -1
			}
		case x != y:
			if x > y {
				return 1
			}
			return -1
		}
	}
	return 0
}
