package secrets

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/zalando/go-keyring"
)

// Errors. They never carry a secret value.
var (
	ErrLocked             = errors.New("secrets are locked")
	ErrWrongPassphrase    = errors.New("wrong passphrase")
	ErrWeakPassphrase     = fmt.Errorf("passphrase must have at least %d characters", MinPassphrase)
	ErrCorrupt            = errors.New("secret file is damaged")
	ErrBadName            = errors.New("bad secret name")
	ErrNotRef             = errors.New("not a reference")
	ErrBadRef             = errors.New("bad reference")
	ErrUnknownBackend     = errors.New("unknown secret store backend")
	ErrKeyringUnavailable = errors.New("OS keyring is not available")
)

// Passphrase rules: refused under MinPassphrase, a warning under
// GoodPassphrase (counted in characters, not bytes).
const (
	MinPassphrase  = 8
	GoodPassphrase = 12
)

// CheckPassphrase refuses a passphrase shorter than MinPassphrase and
// reports weak for one shorter than GoodPassphrase.
func CheckPassphrase(p string) (weak bool, err error) {
	n := utf8.RuneCountInString(p)
	if n < MinPassphrase {
		return false, ErrWeakPassphrase
	}
	return n < GoodPassphrase, nil
}

// Store is the secret store the app uses.
type Store interface {
	Backend() string                                    // "age" | "plain" (later "keyring")
	Locked() bool                                       // age only; others false
	Unlock(pass string) error                           // age only; others no-op
	Lock()                                              // forgets the passphrase, values and the ${cmd:} cache
	Get(name string) (value string, ok bool, err error) // resolves references
	Set(name, value string) error                       // a value, or a reference text
	Delete(name string) error
	Names() []string // from the index, never a backend scan
}

// Backend holds secret values. Get/Set/Delete see values only, never
// references (the Store keeps those in the index).
type Backend interface {
	Name() string
	Locked() bool
	Unlock(pass string) error
	Lock()
	Get(name string) (string, bool, error)
	Set(name, value string) error
	Delete(name string) error // a missing name is not an error
}

// Options configure Open and NewStore.
type Options struct {
	// Dir is termocode's config dir: secrets.json and secrets.age live here.
	Dir string
	// Backend is the secret_store setting: "", "auto", "age", "plain" or
	// "keyring".
	Backend string
	// PlainPath is the plain backend's file (default Dir/ai/credentials.json).
	PlainPath string
	// ScryptWorkFactor is the age scrypt log2 work factor (0: 18).
	ScryptWorkFactor int
	// Workspace is the root whose .env ${dotenv:NAME} reads ("" none).
	Workspace string
	// Getenv reads ${env:NAME} (nil: os.Getenv).
	Getenv func(string) string
	// Register receives every value Get hands out, so callers can hide
	// it in logs (ai.RegisterSecret). nil: none.
	Register func(string)
	// Redact cleans ${cmd:} stderr before it goes into an error. nil: none.
	Redact func(string) string
	// KeyringOverSSH permits probing the OS keyring in an SSH session.
	KeyringOverSSH bool
	// KeyringProbe overrides the system probe, primarily for tests.
	KeyringProbe func() bool
}

// IndexFile and AgeFile are the file names inside Options.Dir.
const (
	IndexFile = "secrets.json"
	AgeFile   = "secrets.age"
)

// Open picks the backend from the setting and the index, records the
// choice in the index and returns the store. An age store starts locked.
func Open(o Options) (Store, error) {
	if o.Dir == "" {
		return nil, errors.New("secrets: no config dir")
	}
	idxPath := filepath.Join(o.Dir, IndexFile)
	idx, err := loadIndex(idxPath)
	if err != nil {
		return nil, err
	}
	name, err := chooseBackend(o.Backend, idx.Backend)
	if err != nil {
		return nil, err
	}
	if (o.Backend == "" || o.Backend == "auto") && idx.Backend == "" {
		probe := o.KeyringProbe
		if probe == nil {
			probe = func() bool { return ProbeKeyring(o.Getenv, o.KeyringOverSSH) }
		}
		if probe() {
			name = "keyring"
		}
	}
	var b Backend
	switch name {
	case "keyring":
		if o.Backend == "keyring" && !probeKeyringOption(o) {
			return nil, fmt.Errorf("%w: no usable OS keyring", ErrKeyringUnavailable)
		}
		b = newKeyringBackend(keyring.Set, keyring.Get, keyring.Delete)
	case "age":
		b = newAgeFile(filepath.Join(o.Dir, AgeFile), o.ScryptWorkFactor)
	case "plain":
		p := o.PlainPath
		if p == "" {
			p = filepath.Join(o.Dir, "ai", "credentials.json")
		}
		b = newPlain(p)
	}
	if idx.Backend != name {
		if err := updateIndex(idxPath, func(i *Index) { i.Backend = name }); err != nil {
			return nil, err
		}
	}
	return NewStore(b, o), nil
}

func probeKeyringOption(o Options) bool {
	if o.KeyringProbe != nil {
		return o.KeyringProbe()
	}
	return ProbeKeyring(o.Getenv, o.KeyringOverSSH)
}

// chooseBackend applies the setting and any previously recorded choice.
func chooseBackend(setting, recorded string) (string, error) {
	switch setting {
	case "age", "plain":
		return setting, nil
	case "keyring":
		return "keyring", nil
	case "", "auto":
		// auto never picks plain, even when the index recorded it.
		if recorded == "age" || recorded == "keyring" {
			return recorded, nil
		}
		return "age", nil
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownBackend, setting)
}

// NewStore wraps a backend with the index in o.Dir and a reference
// resolver. Open uses it; tests use it with an in-memory backend.
func NewStore(b Backend, o Options) Store {
	return &store{
		b:         b,
		indexPath: filepath.Join(o.Dir, IndexFile),
		register:  o.Register,
		res: &Resolver{
			Getenv:    o.Getenv,
			Workspace: o.Workspace,
			Redact:    o.Redact,
		},
	}
}

type store struct {
	mu        sync.Mutex
	b         Backend
	indexPath string
	res       *Resolver
	register  func(string)
}

func (s *store) Backend() string { return s.b.Name() }

func (s *store) Locked() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Locked()
}

func (s *store) Unlock(pass string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Unlock(pass)
}

func (s *store) Lock() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.b.Lock()
	s.res.ForgetCache()
}

func (s *store) Get(name string) (string, bool, error) {
	if err := ValidName(name); err != nil {
		return "", false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.b.Locked() {
		return "", false, ErrLocked
	}
	idx, err := loadIndex(s.indexPath)
	if err != nil {
		return "", false, err
	}
	var v string
	if e, ok := idx.Entries[name]; ok && e.Kind == KindRef {
		r, err := ParseRef(e.Ref)
		if err != nil {
			return "", false, fmt.Errorf("%s: %w", name, err)
		}
		if v, err = s.res.Resolve(r); err != nil {
			return "", false, fmt.Errorf("%s: %w", name, err)
		}
	} else {
		// Not indexed yet (an old plain file) still resolves by name.
		var ok bool
		v, ok, err = s.b.Get(name)
		if err != nil || !ok {
			return "", false, err
		}
	}
	if s.register != nil {
		s.register(v)
	}
	return v, true, nil
}

func (s *store) Set(name, value string) error {
	if err := ValidName(name); err != nil {
		return err
	}
	ref, err := ParseRef(value)
	isRef := err == nil
	if err != nil && !errors.Is(err, ErrNotRef) {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.b.Locked() {
		return ErrLocked
	}
	e := Entry{Kind: KindValue, Backend: s.b.Name(), Updated: clock.Now().UTC()}
	if isRef {
		// A reference replaces a stored value: the value leaves the backend.
		if err := s.b.Delete(name); err != nil {
			return err
		}
		e = Entry{Kind: KindRef, Ref: ref.String(), Updated: e.Updated}
	} else if err := s.b.Set(name, value); err != nil {
		return err
	}
	return updateIndex(s.indexPath, func(i *Index) { i.Entries[name] = e })
}

func (s *store) Delete(name string) error {
	if err := ValidName(name); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.b.Locked() {
		return ErrLocked
	}
	if err := s.b.Delete(name); err != nil {
		return err
	}
	return updateIndex(s.indexPath, func(i *Index) { delete(i.Entries, name) })
}

func (s *store) Names() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, err := loadIndex(s.indexPath)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(idx.Entries))
	for n := range idx.Entries {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Names are dotted words: letters, digits, '_' and '-', parts split by
// single dots, starting with a letter or digit. Case is kept because MCP
// ${secret:NAME} names are often upper case (mcp.secret.GITHUB_PAT).
var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*(\.[A-Za-z0-9_-]+)*$`)

const maxName = 200

// ValidName reports whether name may name a secret.
func ValidName(name string) error {
	if len(name) > maxName || !nameRe.MatchString(name) || strings.Contains(name, "..") {
		return fmt.Errorf("%w: %q", ErrBadName, name)
	}
	return nil
}
