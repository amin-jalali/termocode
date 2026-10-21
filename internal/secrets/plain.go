package secrets

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
)

// plainFile is the opt-in plain backend. It reads and writes the old
// ai/credentials.json shape, so the file stays readable by the
// Credentials code and by older binaries:
//
//	ai.key.<kind>     → keys.<kind>             (string)
//	ai.claude_oauth   → claude_oauth            (JSON object)
//	mcp.secret.<NAME> → mcp_secrets.<NAME>      (string)
//	mcp.oauth.<srv>   → mcp_oauth.<srv>         (JSON object)
//	mcp.account.<srv> → mcp_accounts.<srv>      (string, not secret)
//	anything else     → secrets.<name>          (string)
//
// Fields it does not own are kept as they are.
type plainFile struct {
	mu   sync.Mutex
	path string
}

func newPlain(path string) *plainFile { return &plainFile{path: path} }

func (p *plainFile) Name() string        { return "plain" }
func (p *plainFile) Locked() bool        { return false }
func (p *plainFile) Unlock(string) error { return nil }
func (p *plainFile) Lock()               {}

// slot is where a name lives in the old shape: top-level field, the key
// inside it ("" when the field itself is the value) and whether the
// value is a JSON object (blob) instead of a string.
type slot struct {
	field, key string
	blob       bool
}

func plainSlot(name string) slot {
	switch {
	case name == "ai.claude_oauth":
		return slot{"claude_oauth", "", true}
	case strings.HasPrefix(name, "ai.key."):
		return slot{"keys", strings.TrimPrefix(name, "ai.key."), false}
	case strings.HasPrefix(name, "mcp.secret."):
		return slot{"mcp_secrets", strings.TrimPrefix(name, "mcp.secret."), false}
	case strings.HasPrefix(name, "mcp.oauth."):
		return slot{"mcp_oauth", strings.TrimPrefix(name, "mcp.oauth."), true}
	case strings.HasPrefix(name, "mcp.account."):
		return slot{"mcp_accounts", strings.TrimPrefix(name, "mcp.account."), false}
	}
	return slot{"secrets", name, false}
}

func (p *plainFile) load() (map[string]json.RawMessage, error) {
	top := map[string]json.RawMessage{}
	b, err := os.ReadFile(p.path)
	if errors.Is(err, os.ErrNotExist) {
		return top, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &top); err != nil || top == nil {
		return nil, fmt.Errorf("%w: %s is not valid JSON", ErrCorrupt, p.path)
	}
	return top, nil
}

func (p *plainFile) Get(name string) (string, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	top, err := p.load()
	if err != nil {
		return "", false, err
	}
	s := plainSlot(name)
	raw, ok := top[s.field]
	if ok && s.key != "" {
		var sub map[string]json.RawMessage
		if err := json.Unmarshal(raw, &sub); err != nil {
			return "", false, fmt.Errorf("%w: %s: %s", ErrCorrupt, p.path, s.field)
		}
		raw, ok = sub[s.key]
	}
	if !ok || string(raw) == "null" {
		return "", false, nil
	}
	if s.blob {
		var buf bytes.Buffer
		if err := json.Compact(&buf, raw); err != nil {
			return "", false, fmt.Errorf("%w: %s: %s", ErrCorrupt, p.path, name)
		}
		return buf.String(), true, nil
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", false, fmt.Errorf("%w: %s: %s is not a string", ErrCorrupt, p.path, name)
	}
	return v, true, nil
}

func (p *plainFile) Set(name, value string) error {
	s := plainSlot(name)
	var raw json.RawMessage
	if s.blob {
		v := strings.TrimSpace(value)
		if !strings.HasPrefix(v, "{") || !json.Valid([]byte(v)) {
			return fmt.Errorf("%s must be a JSON object", name)
		}
		raw = json.RawMessage(v)
	} else {
		b, err := json.Marshal(value)
		if err != nil {
			return err
		}
		raw = b
	}
	return p.change(s, &raw)
}

func (p *plainFile) Delete(name string) error {
	return p.change(plainSlot(name), nil)
}

// change sets (raw != nil) or removes (raw == nil) one slot and writes
// the file back; empty maps are dropped, as omitempty did.
func (p *plainFile) change(s slot, raw *json.RawMessage) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	top, err := p.load()
	if err != nil {
		return err
	}
	if s.key == "" {
		if raw == nil {
			if _, ok := top[s.field]; !ok {
				return nil
			}
			delete(top, s.field)
		} else {
			top[s.field] = *raw
		}
	} else {
		sub := map[string]json.RawMessage{}
		if old, ok := top[s.field]; ok {
			if err := json.Unmarshal(old, &sub); err != nil || sub == nil {
				return fmt.Errorf("%w: %s: %s", ErrCorrupt, p.path, s.field)
			}
		}
		if raw == nil {
			if _, ok := sub[s.key]; !ok {
				return nil
			}
			delete(sub, s.key)
		} else {
			sub[s.key] = *raw
		}
		if len(sub) == 0 {
			delete(top, s.field)
		} else {
			b, err := json.Marshal(sub)
			if err != nil {
				return err
			}
			top[s.field] = b
		}
	}
	b, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(p.path, b, true)
}
