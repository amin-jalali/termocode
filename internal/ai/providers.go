package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/amin-jalali/termocode/internal/cfgfile"
)

// ── Connected providers (ai/providers.json) ───────────────────────────
//
// providers.json lists the AIs the user connected, one entry per kind:
// how (an API key in the secret store, a vendor CLI's own sign-in, a
// local server, a server URL) and, for the last two, where. It never
// holds a key: keys stay in the secret store, sign-ins in the CLI.

// How a provider is connected (ProviderEntry.Method).
const (
	MethodKey   = "key"   // an API key in the secret store
	MethodCLI   = "cli"   // a vendor CLI's own subscription sign-in
	MethodLocal = "local" // a server on this machine (Ollama, LM Studio)
	MethodURL   = "url"   // a server URL (openai-compatible)
)

// ProviderEntry is one connected provider.
type ProviderEntry struct {
	Kind    string `json:"-"` // the key of the entry in providers.json
	Method  string `json:"method"`
	CLI     string `json:"cli,omitempty"` // MethodCLI: CLIClaude or CLICodex
	BaseURL string `json:"base_url,omitempty"`
	Title   string `json:"title,omitempty"`    // a name the user gave (Other server)
	AddedAt int64  `json:"added_at,omitempty"` // unix seconds
}

// ProvidersPath is <config>/ai/providers.json.
func ProvidersPath() (string, error) {
	d, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "ai", "providers.json"), nil
}

// LoadProviders reads providers.json by kind. A missing file is an error
// that wraps fs.ErrNotExist (the caller then derives the list); entries
// of an unknown kind or method are left out.
func LoadProviders(path string) (map[string]ProviderEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Providers map[string]ProviderEntry `json:"providers"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON", path)
	}
	out := map[string]ProviderEntry{}
	for kind, p := range doc.Providers {
		p.Kind = kind
		if checkProvider(p) == nil {
			out[kind] = p
		}
	}
	return out, nil
}

// checkProvider reports why p cannot be saved (nil = fine).
func checkProvider(p ProviderEntry) error {
	switch {
	case !knownKind(p.Kind):
		return fmt.Errorf("unknown provider %q (%s)", p.Kind, strings.Join(Kinds, ", "))
	case p.Method != MethodKey && p.Method != MethodCLI && p.Method != MethodLocal && p.Method != MethodURL:
		return fmt.Errorf("unknown method %q (key, cli, local or url)", p.Method)
	case p.Method == MethodCLI && CLITitle(p.CLI) == "":
		return fmt.Errorf("unknown cli %q (claude or codex)", p.CLI)
	}
	return CheckSlotBaseURL(p.BaseURL)
}

// SaveProvider writes providers.<kind> at path (one entry per kind: it
// replaces the kind's entry). The entry's unknown fields stay, and so do
// the other entries and keys. An entry that does not check is not
// written.
func SaveProvider(path string, p ProviderEntry) error {
	p.BaseURL = strings.TrimSpace(p.BaseURL)
	if err := checkProvider(p); err != nil {
		return err
	}
	return cfgfile.UpdateRaw(path, func(doc map[string]json.RawMessage) error {
		provs, err := rawObject(doc["providers"], "providers")
		if err != nil {
			return err
		}
		entry, err := rawObject(provs[p.Kind], "providers."+p.Kind)
		if err != nil {
			return err
		}
		fields, _ := json.Marshal(p)
		var set map[string]json.RawMessage
		_ = json.Unmarshal(fields, &set)
		for _, k := range []string{"method", "cli", "base_url", "title", "added_at"} {
			if v, ok := set[k]; ok {
				entry[k] = v
			} else {
				delete(entry, k)
			}
		}
		if provs[p.Kind], err = json.Marshal(entry); err != nil {
			return err
		}
		doc["providers"], err = json.Marshal(provs)
		return err
	})
}

// SaveProviders writes every entry of ps (in kind order).
func SaveProviders(path string, ps map[string]ProviderEntry) error {
	kinds := make([]string, 0, len(ps))
	for k := range ps {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	var errs []error
	for _, k := range kinds {
		p := ps[k]
		p.Kind = k
		if err := SaveProvider(path, p); err != nil {
			errs = append(errs, err)
		}
	}
	if len(ps) == 0 { // an empty list is still written: it is not derived again
		errs = append(errs, cfgfile.UpdateRaw(path, func(doc map[string]json.RawMessage) error {
			if _, ok := doc["providers"]; !ok {
				doc["providers"] = json.RawMessage("{}")
			}
			return nil
		}))
	}
	return errors.Join(errs...)
}

// RemoveProvider deletes providers.<kind> at path; everything else stays.
// A kind that is not there is no error.
func RemoveProvider(path, kind string) error {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return cfgfile.UpdateRaw(path, func(doc map[string]json.RawMessage) error {
		provs, err := rawObject(doc["providers"], "providers")
		if err != nil {
			return err
		}
		delete(provs, kind)
		doc["providers"], err = json.Marshal(provs)
		return err
	})
}

// EnvKeyVar names the vendor environment variable of kind's API key
// ("" = the kind takes none).
func EnvKeyVar(kind string) string { return envKeyFor(kind) }

// EnvOnlyKinds lists, sorted, the kinds whose key comes from their vendor
// environment variable and that provs does not hold: they work without
// being connected, and only the environment can take them away.
func EnvOnlyKinds(provs map[string]ProviderEntry, getenv func(string) string) []string {
	if getenv == nil {
		getenv = os.Getenv
	}
	var out []string
	for _, kind := range Kinds {
		if _, ok := provs[kind]; ok {
			continue
		}
		if e := envKeyFor(kind); e != "" && strings.TrimSpace(getenv(e)) != "" {
			out = append(out, kind)
		}
	}
	sort.Strings(out)
	return out
}

// CLIKind is the provider kind whose subscription a step CLI signs in to.
func CLIKind(cli string) string {
	switch cli {
	case CLIClaude:
		return KindAnthropic
	case CLICodex:
		return KindOpenAI
	}
	return ""
}

// DeriveProviders is the provider list of a setup made before
// providers.json: a stored key of a kind → MethodKey; a CLI cliReady
// reports signed in → MethodCLI of its kind (a key wins: one method per
// provider); ai_provider ollama / lmstudio → MethodLocal and
// openai-compatible → MethodURL with ai_base_url. cliReady nil: no CLI
// is checked.
func DeriveProviders(s Settings, c Credentials, cliReady func(cli string) bool) map[string]ProviderEntry {
	out := map[string]ProviderEntry{}
	for kind, key := range c.Keys {
		if key != "" && knownKind(kind) && kind != KindClaudeOAuth {
			out[kind] = ProviderEntry{Kind: kind, Method: MethodKey}
		}
	}
	if cliReady != nil {
		for _, cli := range CLIs {
			kind := CLIKind(cli)
			if _, ok := out[kind]; !ok && cliReady(cli) {
				out[kind] = ProviderEntry{Kind: kind, Method: MethodCLI, CLI: cli}
			}
		}
	}
	kind := strings.ToLower(strings.TrimSpace(s.Provider))
	base := strings.TrimSpace(s.BaseURL)
	if CheckSlotBaseURL(base) != nil {
		base = ""
		if kind == KindCompatible {
			return out
		}
	}
	switch kind {
	case KindOllama, KindLMStudio:
		out[kind] = ProviderEntry{Kind: kind, Method: MethodLocal, BaseURL: base}
	case KindCompatible:
		if base != "" {
			out[kind] = ProviderEntry{Kind: kind, Method: MethodURL, BaseURL: base}
		}
	}
	return out
}

// CostTag is how a provider is paid for, as the model picker tags its
// group: "your plan", "API · pay per use" or "local · free".
func CostTag(p ProviderEntry) string {
	switch {
	case p.Method == MethodCLI:
		return "your plan"
	case p.Method == MethodLocal, p.Method == MethodURL && localURL(p.BaseURL):
		return "local · free"
	}
	return "API · pay per use"
}

// MethodLabel is how a provider is connected, for people.
func MethodLabel(p ProviderEntry) string {
	switch p.Method {
	case MethodKey:
		return "API key"
	case MethodCLI:
		return "sign-in via " + CLITitle(p.CLI)
	case MethodLocal:
		return "on this machine"
	case MethodURL:
		return "server URL"
	}
	return p.Method
}

// localURL reports whether raw names this machine.
func localURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	h := u.Hostname()
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}
