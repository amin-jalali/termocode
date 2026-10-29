package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// "Paste a config…" in the Connect wizard: a Claude Desktop / Claude
// Code / Cursor mcpServers block becomes mcp.json entries. Literal
// tokens move to credentials.json (mcp_secrets) and the entry gets a
// ${secret:NAME} instead, so mcp.json never holds a secret.

// Imported is one pasted server, ready for AddServer.
type Imported struct {
	Name    string
	Entry   map[string]any
	Secrets map[string]string // mcp_secrets name → value
}

// ParseImport reads a pasted block: {"mcpServers": {…}}, the servers map
// alone, or one "name": {…} pair. problems name the entries that can not
// run (they are left out); err is for text that is not a block at all.
func ParseImport(text string) ([]Imported, []string, error) {
	text = strings.TrimSpace(text)
	text = strings.TrimSuffix(text, ",")
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &top); err != nil {
		if err2 := json.Unmarshal([]byte("{"+text+"}"), &top); err2 != nil {
			return nil, nil, errors.New("that is not an mcpServers block (JSON)")
		}
	}
	servers := top
	if raw, ok := top["mcpServers"]; ok {
		servers = nil
		if err := json.Unmarshal(raw, &servers); err != nil {
			return nil, nil, errors.New("mcpServers is not an object")
		}
	}
	names := make([]string, 0, len(servers))
	for n := range servers {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []Imported
	var problems []string
	for _, name := range names {
		var e map[string]any
		if err := json.Unmarshal(servers[name], &e); err != nil {
			problems = append(problems, name+": not an object")
			continue
		}
		im := Imported{Name: name, Entry: e, Secrets: map[string]string{}}
		im.moveSecrets()
		b, _ := json.Marshal(im.Entry)
		if s := parseEntry(name, b); s.Problem != "" {
			problems = append(problems, name+": "+s.Problem)
			continue
		}
		out = append(out, im)
	}
	if len(out) == 0 && len(problems) == 0 {
		return nil, nil, errors.New("the block has no servers")
	}
	return out, problems, nil
}

// secretName is NAME for a server's key ("github", "Authorization" →
// "github_authorization").
func secretName(server, key string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(server + "_" + key) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// moveSecrets replaces literal tokens in headers, env and oauth with
// ${secret:NAME} and keeps the values in Secrets.
func (im *Imported) moveSecrets() {
	// JSON keys match struct fields in any case ("Headers", "ENV"), so
	// every spelling of a field is cleaned.
	for _, key := range sortedAnyKeys(im.Entry) {
		field := strings.ToLower(key)
		if field != "headers" && field != "env" {
			continue
		}
		m, ok := im.Entry[key].(map[string]any)
		if !ok {
			continue
		}
		for _, k := range sortedAnyKeys(m) {
			s, ok := m[k].(string)
			if !ok || !secretLiteralRe.MatchString(refRe.ReplaceAllString(s, "")) {
				continue
			}
			name := secretName(im.Name, k)
			if scheme, tok, ok := strings.Cut(s, " "); ok && field == "headers" && !strings.Contains(tok, " ") &&
				(strings.EqualFold(scheme, "Bearer") || strings.EqualFold(scheme, "Sentry-Bearer") || strings.EqualFold(scheme, "Basic") || strings.EqualFold(scheme, "token")) {
				im.Secrets[name] = tok
				m[k] = scheme + " ${secret:" + name + "}"
				continue
			}
			im.Secrets[name] = s
			m[k] = "${secret:" + name + "}"
		}
	}
	for _, key := range sortedAnyKeys(im.Entry) {
		o, ok := im.Entry[key].(map[string]any)
		if !ok || !strings.EqualFold(key, "oauth") {
			continue
		}
		for _, k := range sortedAnyKeys(o) {
			s, ok := o[k].(string)
			if !ok || !strings.EqualFold(k, "client_secret") || s == "" || strings.Contains(s, "${") {
				continue
			}
			name := secretName(im.Name, "client_secret")
			im.Secrets[name] = s
			o[k] = "${secret:" + name + "}"
		}
	}
}

// sortedAnyKeys returns the keys of m in order, so secret names do not
// depend on map order.
func sortedAnyKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ImportSummary is a one-line result for the wizard.
func ImportSummary(added, skipped []string) string {
	s := ""
	if len(added) > 0 {
		s = fmt.Sprintf("Added %s.", strings.Join(added, ", "))
	}
	if len(skipped) > 0 {
		s += fmt.Sprintf(" Skipped %s.", strings.Join(skipped, "; "))
	}
	return strings.TrimSpace(s)
}
