package mcp

import (
	"fmt"
	"strings"

	"github.com/amin-jalali/termocode/internal/ai"
)

// RenameServer renames an mcp.json entry and everything stored for it
// (CX-7, "Add another account"): its ${secret:OLD_token} reference, the
// OAuth sign-in, the token secret and the account label. A name that
// exists is an error.
func RenameServer(path string, cs CredStore, old, name string) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("%q is not a server name", name)
	}
	err := editServers(path, func(servers map[string]any) error {
		e, ok := servers[old].(map[string]any)
		if !ok {
			return fmt.Errorf("%s has no server named %s", path, old)
		}
		if _, taken := servers[name]; taken {
			return fmt.Errorf("%s already has a server named %s", path, name)
		}
		if h, ok := e["headers"].(map[string]any); ok {
			for k, v := range h {
				if s, ok := v.(string); ok {
					h[k] = strings.ReplaceAll(s, "${secret:"+old+"_token}", "${secret:"+name+"_token}")
				}
			}
		}
		delete(servers, old)
		servers[name] = e
		return nil
	})
	if err != nil {
		return err
	}
	return cs.update(func(cr *ai.Credentials) {
		if e, ok := cr.MCPOAuth[old]; ok {
			cr.MCPOAuth[name] = e
			delete(cr.MCPOAuth, old)
		}
		if a, ok := cr.MCPAccounts[old]; ok {
			cr.MCPAccounts[name] = a
			delete(cr.MCPAccounts, old)
		}
		if v, ok := cr.MCPSecrets[old+"_token"]; ok {
			cr.MCPSecrets[name+"_token"] = v
			delete(cr.MCPSecrets, old+"_token")
		}
	})
}

// EntryName makes an entry name from a preset and an account
// ("github", "Perkino Deals" → "github-perkino-deals").
func EntryName(preset, account string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(account) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	a := strings.Trim(b.String(), "-")
	n := preset + "-" + a
	if len(n) > 40 {
		n = strings.TrimRight(n[:40], "-")
	}
	if a == "" || !nameRe.MatchString(n) {
		return ""
	}
	return n
}
