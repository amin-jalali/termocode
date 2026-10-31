package mcp

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// TeammateJSON is one entry as an mcpServers block to give a teammate
// (MCPX-28): the entry as written, with every literal token (a value
// that looks like one, or that isSecret knows) turned into a new
// ${secret:NAME} reference, never the value. ${secret:} references stay;
// the personal "auth" sign-in is dropped. names are the secrets the
// teammate sets.
func TeammateJSON(s Server, isSecret func(string) bool) (out string, names []string, err error) {
	var e map[string]any
	if err := json.Unmarshal(s.raw, &e); err != nil || e == nil {
		return "", nil, fmt.Errorf("the entry of %s is not a JSON object", s.Name)
	}
	delete(e, "auth")
	secret := func(v string) bool {
		if isSecret != nil && isSecret(v) {
			return true
		}
		return secretLiteralRe.MatchString(refRe.ReplaceAllString(v, ""))
	}
	ref := func(v, name string) string {
		r := "${secret:" + teammateSecretName(name) + "}"
		if low := strings.ToLower(v); strings.HasPrefix(low, "bearer ") || strings.HasPrefix(low, "token ") {
			return v[:strings.IndexByte(v, ' ')+1] + r
		}
		return r
	}
	for _, key := range []string{"env", "headers"} {
		m, _ := e[key].(map[string]any)
		for k, v := range m {
			if str, ok := v.(string); ok && secret(str) {
				m[k] = ref(str, s.Name+"_"+k)
			}
		}
	}
	if args, ok := e["args"].([]any); ok {
		for i, v := range args {
			if str, ok := v.(string); ok && secret(str) {
				args[i] = ref(str, fmt.Sprintf("%s_arg_%d", s.Name, i+1))
			}
		}
	}
	if o, ok := e["oauth"].(map[string]any); ok {
		if str, ok := o["client_secret"].(string); ok && strings.TrimSpace(refRe.ReplaceAllString(str, "")) != "" {
			o["client_secret"] = ref(str, s.Name+"_client_secret")
		}
	}
	b, err := json.MarshalIndent(map[string]any{"mcpServers": map[string]any{s.Name: e}}, "", "  ")
	if err != nil {
		return "", nil, err
	}
	back, _, err := ParseFile(b, s.Source, s.SourcePath)
	if err != nil || len(back) != 1 {
		return "", nil, fmt.Errorf("the copy of %s does not load", s.Name)
	}
	names = back[0].SecretNames()
	sort.Strings(names)
	return string(b) + "\n", names, nil
}

// teammateSecretName is a ${secret:} name from a key: lower case, other
// runes as "_".
func teammateSecretName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}
