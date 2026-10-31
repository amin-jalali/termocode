package secrets

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ── Migration of the old plain ai/credentials.json (plan U19 §3.6) ──────
//
//  1. FromPlain reads the old file into store names and values.
//  2. Migrate opens the chosen backend (age or keyring) and writes them.
//  3. Verify reads every name back and compares the bytes.
//  4. Only then Remove overwrites the old file with zeros of the same
//     length, syncs it, deletes it and writes the empty marker
//     credentials.json.migrated (0600), so this binary knows the move
//     is done and never asks again.
//
// Any failure before step 4 leaves the plain file untouched and the
// index as it was, so the plain file stays the backend. An interrupted
// move (the process died between 2 and 4) leaves both copies; Finish
// completes step 4 on the next start once the store holds the same
// names and values.

// ErrNothingToMove: the plain file holds no secret.
var ErrNothingToMove = errors.New("ai/credentials.json holds no secrets to move")

// ErrNotRemoved: the secrets are in the new store, but the old file
// could not be removed; the next start finishes it (Finish).
var ErrNotRemoved = errors.New("secrets moved, but the old file could not be removed")

// Test hooks: removeFn deletes the zeroed file, verifyFn is the read-back
// check Migrate runs.
var (
	removeFn = os.Remove
	verifyFn = Verify
)

// MarkerSuffix is added to the plain file's path for the marker.
const MarkerSuffix = ".migrated"

// MarkerPath is the marker written after a finished move.
func MarkerPath(plainPath string) string { return plainPath + MarkerSuffix }

// Migrated reports a finished move (the marker exists).
func Migrated(plainPath string) bool {
	_, err := os.Stat(MarkerPath(plainPath))
	return err == nil
}

// Plain is what the old file holds: store names and values, the MCP
// account labels (mcp.account.<server>, not secret) included.
type Plain struct {
	Values map[string]string
}

// Counts are the kinds of names in a Plain, for the card (never values).
type Counts struct {
	AIKeys, ClaudeOAuth, MCPSecrets, MCPOAuth, Other, Accounts int
}

// Secrets is the number of secret names (accounts are not secret).
func (c Counts) Secrets() int {
	return c.AIKeys + c.ClaudeOAuth + c.MCPSecrets + c.MCPOAuth + c.Other
}

// String is "2 AI keys · Claude sign-in · 3 MCP secrets · 2 sign-ins".
func (c Counts) String() string {
	plural := func(n int, one, many string) string {
		if n == 1 {
			return "1 " + one
		}
		return strconv.Itoa(n) + " " + many
	}
	var parts []string
	if c.AIKeys > 0 {
		parts = append(parts, plural(c.AIKeys, "AI key", "AI keys"))
	}
	if c.ClaudeOAuth > 0 {
		parts = append(parts, "Claude sign-in")
	}
	if c.MCPSecrets > 0 {
		parts = append(parts, plural(c.MCPSecrets, "MCP secret", "MCP secrets"))
	}
	if c.MCPOAuth > 0 {
		parts = append(parts, plural(c.MCPOAuth, "sign-in", "sign-ins"))
	}
	if c.Other > 0 {
		parts = append(parts, plural(c.Other, "other secret", "other secrets"))
	}
	return strings.Join(parts, " · ")
}

// Counts sorts the names by kind.
func (p Plain) Counts() Counts {
	var c Counts
	for n := range p.Values {
		switch {
		case n == "ai.claude_oauth":
			c.ClaudeOAuth++
		case strings.HasPrefix(n, "ai.key."):
			c.AIKeys++
		case strings.HasPrefix(n, "mcp.secret."):
			c.MCPSecrets++
		case strings.HasPrefix(n, "mcp.oauth."):
			c.MCPOAuth++
		case PublicName(n):
			c.Accounts++
		default:
			c.Other++
		}
	}
	return c
}

// plainFields are the old file's top-level fields and the name prefix of
// their entries ("" for claude_oauth, which is one value).
var plainFields = map[string]string{
	"keys":         "ai.key.",
	"claude_oauth": "",
	"mcp_secrets":  "mcp.secret.",
	"mcp_oauth":    "mcp.oauth.",
	"mcp_accounts": "mcp.account.",
	"secrets":      "",
}

// FromPlain reads the old ai/credentials.json. A missing file gives
// nothing. A field or name it can not move is an error, so Remove never
// deletes something that was not copied. Empty values are left out.
func FromPlain(path string) (Plain, error) {
	p := newPlain(path)
	out := Plain{Values: map[string]string{}}
	top, err := p.load()
	if err != nil {
		return out, err
	}
	var names []string
	for field, raw := range top {
		prefix, ok := plainFields[field]
		if !ok {
			return out, fmt.Errorf("%s: unknown field %q, not moved", path, field)
		}
		if string(raw) == "null" {
			continue
		}
		if field == "claude_oauth" {
			names = append(names, "ai.claude_oauth")
			continue
		}
		var sub map[string]json.RawMessage
		if err := json.Unmarshal(raw, &sub); err != nil {
			return out, fmt.Errorf("%w: %s: %s", ErrCorrupt, path, field)
		}
		for k := range sub {
			names = append(names, prefix+k)
		}
	}
	for _, n := range names {
		if err := ValidName(n); err != nil {
			return out, fmt.Errorf("%s: %w", path, err)
		}
		v, ok, err := p.Get(n)
		if err != nil {
			return out, err
		}
		if ok && v != "" {
			out.Values[n] = v
		}
	}
	return out, nil
}

// SetAll writes values to s: one backend write where the backend allows
// it (the age file encrypts once), then one index write.
func SetAll(s Store, values map[string]string) error {
	plainValues := map[string]string{}
	var refs []string
	for n, v := range values {
		if err := ValidName(n); err != nil {
			return err
		}
		if _, err := ParseRef(v); err == nil {
			refs = append(refs, n)
		} else {
			plainValues[n] = v
		}
	}
	if st, ok := s.(*store); ok {
		if err := st.setAll(plainValues); err != nil {
			return err
		}
	} else {
		for _, n := range sortedNames(plainValues) {
			if err := s.Set(n, plainValues[n]); err != nil {
				return err
			}
		}
	}
	sort.Strings(refs)
	for _, n := range refs {
		if err := s.Set(n, values[n]); err != nil {
			return err
		}
	}
	return nil
}

// Verify reads every name back from s and compares the bytes. Its error
// names the secret, never the value.
func Verify(s Store, values map[string]string) error {
	for _, n := range sortedNames(values) {
		v, ok, err := s.Get(n)
		if err != nil {
			return fmt.Errorf("check %s: %w", n, err)
		}
		if !ok {
			return fmt.Errorf("check: %s is missing after the copy", n)
		}
		if subtle.ConstantTimeCompare([]byte(v), []byte(values[n])) != 1 {
			return fmt.Errorf("check: %s differs after the copy", n)
		}
	}
	return nil
}

func sortedNames(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for n := range m {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Remove overwrites the plain file with zeros of the same length, syncs
// it to disk, deletes it and writes the empty marker (0600).
func Remove(path string) error {
	if err := wipe(path); err != nil {
		return err
	}
	return writeFileAtomic(MarkerPath(path), nil, false)
}

// wipe overwrites a file with zeros of the same length, syncs it to
// disk and deletes it.
func wipe(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	if _, err := f.WriteAt(make([]byte, fi.Size()), 0); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := removeFn(path); err != nil {
		return err
	}
	if d, err := os.Open(filepath.Dir(path)); err == nil {
		_ = d.Sync() // the delete reaches the disk (best effort)
		d.Close()
	}
	return nil
}

// MigrateOptions configure Migrate. Options are those of Open (Dir,
// PlainPath, ScryptWorkFactor, KeyringProbe, Register …); Backend and
// KeepPlainFile are set by Migrate.
type MigrateOptions struct {
	Options
	// To is the new backend: "age" or "keyring".
	To string
	// Pass is the age passphrase (a new file is created with it, an
	// existing one must open with it). The caller zeroes it.
	Pass []byte
	// Commit runs after the check and before the old file is removed:
	// the caller starts using the new store there.
	Commit func(Store)
	// RemovePlain also wipes the plain file after the check. Off by
	// default: the file is the user's to delete (see Finish).
	RemovePlain bool
}

// Migrate moves the plain file's secrets to the backend o.To and
// returns the open store. On a failure before the delete step the plain
// file, the index and the backend choice are as before (a new
// secrets.age is removed again; keyring items already written stay, but
// nothing points at them). After Commit, a failed delete returns the
// store and ErrNotRemoved.
func Migrate(o MigrateOptions) (Store, error) {
	if o.Dir == "" {
		return nil, errors.New("secrets: no config dir")
	}
	if o.PlainPath == "" {
		o.PlainPath = filepath.Join(o.Dir, "ai", "credentials.json")
	}
	switch o.To {
	case "age":
		if _, err := os.Stat(filepath.Join(o.Dir, AgeFile)); errors.Is(err, os.ErrNotExist) {
			if _, err := CheckPassphrase(string(o.Pass)); err != nil {
				return nil, err
			}
		}
	case "keyring":
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownBackend, o.To)
	}
	p, err := FromPlain(o.PlainPath)
	if err != nil {
		return nil, err
	}
	if p.Counts().Secrets() == 0 {
		return nil, ErrNothingToMove
	}

	idxPath := filepath.Join(o.Dir, IndexFile)
	agePath := filepath.Join(o.Dir, AgeFile)
	idxBefore, idxErr := os.ReadFile(idxPath)
	_, ageErr := os.Stat(agePath)
	var s Store
	rollback := func(err error) (Store, error) {
		if s != nil {
			s.Lock()
		}
		if idxErr == nil {
			_ = writeFileAtomic(idxPath, idxBefore, false)
		} else if errors.Is(idxErr, os.ErrNotExist) {
			_ = os.Remove(idxPath)
		}
		if o.To == "age" && errors.Is(ageErr, os.ErrNotExist) {
			_ = os.Remove(agePath)
		}
		return nil, err
	}

	opts := o.Options
	opts.Backend, opts.KeepPlainFile = o.To, false
	if s, err = Open(opts); err != nil {
		return rollback(err)
	}
	if o.To == "age" {
		if err := s.Unlock(string(o.Pass)); err != nil {
			return rollback(err)
		}
	}
	if err := SetAll(s, p.Values); err != nil {
		return rollback(err)
	}
	if err := verifyFn(s, p.Values); err != nil {
		return rollback(err)
	}
	if o.Commit != nil {
		o.Commit(s)
	}
	if o.RemovePlain { // never by default: the user decides (FIX-24)
		if err := Remove(o.PlainPath); err != nil {
			return s, fmt.Errorf("%w: %v", ErrNotRemoved, err)
		}
	}
	return s, nil
}

// Finish completes an interrupted move: s is an open keyring or age
// store that holds every name of the plain file. It checks the values
// and then removes the plain file (done=true). A store that misses a
// name is not a finished copy: nothing happens. A value that differs is
// an error and the plain file stays.
func Finish(s Store, plainPath string) (done bool, err error) {
	if s == nil || s.Backend() == "plain" || s.Locked() {
		return false, nil
	}
	p, err := FromPlain(plainPath)
	if err != nil || len(p.Values) == 0 {
		return false, err
	}
	have := map[string]bool{}
	for _, n := range s.Names() {
		have[n] = true
	}
	for n := range p.Values {
		if !have[n] {
			return false, nil
		}
	}
	if err := verifyFn(s, p.Values); err != nil {
		return false, err
	}
	if err := Remove(plainPath); err != nil {
		return false, err
	}
	return true, nil
}

// LaterCount is how often the migration card was answered with "later".
func LaterCount(dir string) int {
	idx, err := loadIndex(filepath.Join(dir, IndexFile))
	if err != nil {
		return 0
	}
	return idx.MigrateLater
}

// AddLater counts one more "later" and returns the new count.
func AddLater(dir string) (int, error) {
	path := filepath.Join(dir, IndexFile)
	if err := updateIndex(path, func(i *Index) { i.MigrateLater++ }); err != nil {
		return 0, err
	}
	return LaterCount(dir), nil
}
