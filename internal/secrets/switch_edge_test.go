package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

// canary is a fake secret built from parts, so no scanner sees a real
// looking key in the source.
var canary = "sk-" + "canary-" + "7731"

// fakeBackend is an in-memory Backend whose calls can fail.
type fakeBackend struct {
	data            map[string]string
	getErr, setErr  error
	delErr          error
	missing, differ map[string]bool
	locked          bool
	unlockErr       error
	deleted         []string
}

func newFakeBackend() *fakeBackend { return &fakeBackend{data: map[string]string{}} }

func (f *fakeBackend) Name() string { return "fake" }
func (f *fakeBackend) Locked() bool { return f.locked }
func (f *fakeBackend) Unlock(string) error {
	if f.unlockErr != nil {
		return f.unlockErr
	}
	f.locked = false
	return nil
}
func (f *fakeBackend) Lock() {}
func (f *fakeBackend) Get(n string) (string, bool, error) {
	if f.getErr != nil {
		return "", false, f.getErr
	}
	if f.missing[n] {
		return "", false, nil
	}
	if f.differ[n] {
		return f.data[n] + "x", true, nil
	}
	v, ok := f.data[n]
	return v, ok, nil
}
func (f *fakeBackend) Set(n, v string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.data[n] = v
	return nil
}
func (f *fakeBackend) Delete(n string) error {
	if f.delErr != nil {
		return f.delErr
	}
	f.deleted = append(f.deleted, n)
	delete(f.data, n)
	return nil
}

// otherStore is a Store that is not the package's own store.
type otherStore struct{ Store }

func noValueIn(t *testing.T, err error) {
	t.Helper()
	if err != nil && (strings.Contains(err.Error(), canary) || strings.Contains(err.Error(), "SWITCH-SECRET")) {
		t.Fatalf("the error shows a secret value: %v", err)
	}
}

// Spec (U19 SS-8): Switch refuses bad input before it touches anything.
func TestSwitchRejectsBadInput(t *testing.T) {
	from, dir := switchFrom(t, "age")
	idxBefore := readFile(t, filepath.Join(dir, IndexFile))

	if _, err := Switch(otherStore{from}, SwitchOptions{RemovePlain: true, Options: switchOpts(dir), To: "plain"}); err == nil || !strings.Contains(err.Error(), "can not be switched") {
		t.Fatalf("foreign store: err = %v", err)
	}
	o := switchOpts(dir)
	o.Dir = ""
	if _, err := Switch(from, SwitchOptions{RemovePlain: true, Options: o, To: "plain"}); err == nil || !strings.Contains(err.Error(), "no config dir") {
		t.Fatalf("no dir: err = %v", err)
	}
	if _, err := Switch(from, SwitchOptions{RemovePlain: true, Options: switchOpts(dir), To: "vault"}); !errors.Is(err, ErrUnknownBackend) {
		t.Fatalf("unknown backend: err = %v", err)
	}
	if got := readFile(t, filepath.Join(dir, IndexFile)); got != idxBefore {
		t.Fatal("a refused switch changed the index")
	}
	if from.Locked() {
		t.Fatal("a refused switch locked the old store")
	}
}

// Spec (U19 SS-8): a damaged index stops the switch; it is never
// overwritten.
func TestSwitchCorruptIndex(t *testing.T) {
	from, dir := switchFrom(t, "age")
	idx := filepath.Join(dir, IndexFile)
	if err := os.WriteFile(idx, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Switch(from, SwitchOptions{RemovePlain: true, Options: switchOpts(dir), To: "plain"}); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("err = %v, want ErrCorrupt", err)
	}
	if got := readFile(t, idx); got != "{not json" {
		t.Fatalf("the damaged index was overwritten: %q", got)
	}
}

// Spec (U19 SS-8): with no PlainPath the plain file goes to
// Dir/ai/credentials.json.
func TestSwitchToPlainDefaultPath(t *testing.T) {
	from, dir := switchFrom(t, "age")
	o := switchOpts(dir)
	o.PlainPath = ""
	s, err := Switch(from, SwitchOptions{RemovePlain: true, Options: o, To: "plain"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Backend() != "plain" {
		t.Fatalf("backend %s", s.Backend())
	}
	if b := readFile(t, filepath.Join(dir, "ai", "credentials.json")); !strings.Contains(b, "sk-SWITCH-SECRET-1") {
		t.Fatalf("the default plain file lacks the key:\n%s", b)
	}
}

// Spec (U19 SS-8): a keyring that fails to read stops the switch; the
// error names the secret, never a value.
func TestSwitchOldKeyringReadFails(t *testing.T) {
	from, dir := switchFrom(t, "keyring")
	keyring.MockInitWithError(errors.New("dbus gone"))
	t.Cleanup(keyring.MockInit)
	_, err := Switch(from, SwitchOptions{RemovePlain: true, Options: switchOpts(dir), To: "plain"})
	if err == nil || !strings.Contains(err.Error(), "read ") || !strings.Contains(err.Error(), "dbus gone") {
		t.Fatalf("err = %v", err)
	}
	noValueIn(t, err)
	gone(t, switchOpts(dir).PlainPath)
}

// Spec (U19 SS-8): a keyring that refuses a write undoes the switch.
func TestSwitchKeyringWriteFails(t *testing.T) {
	from, dir := switchFrom(t, "age")
	idxBefore := readFile(t, filepath.Join(dir, IndexFile))
	keyring.MockInitWithError(errors.New("keyring is full"))
	t.Cleanup(keyring.MockInit)
	_, err := Switch(from, SwitchOptions{RemovePlain: true, Options: switchOpts(dir), To: "keyring"})
	if err == nil || !strings.Contains(err.Error(), "write ") || !strings.Contains(err.Error(), "keyring is full") {
		t.Fatalf("err = %v", err)
	}
	noValueIn(t, err)
	if got := readFile(t, filepath.Join(dir, IndexFile)); got != idxBefore {
		t.Fatal("the index changed")
	}
	if v, _, err := from.Get("ai.key.openai"); v != "sk-SWITCH-SECRET-1" || err != nil {
		t.Fatalf("old store Get = %q %v", v, err)
	}
}

// Spec (U19 SS-8): when the keyring is skipped for no named reason the
// error still says why.
func TestSwitchKeyringNoAnswer(t *testing.T) {
	from, dir := switchFrom(t, "age")
	o := switchOpts(dir)
	o.Getenv = func(k string) string {
		if k == "DBUS_SESSION_BUS_ADDRESS" {
			return "unix:path=/fake"
		}
		return ""
	}
	o.KeyringProbe = func() bool { return false }
	_, err := Switch(from, SwitchOptions{RemovePlain: true, Options: o, To: "keyring"})
	if !errors.Is(err, ErrKeyringUnavailable) || !strings.Contains(err.Error(), "it did not answer") {
		t.Fatalf("err = %v", err)
	}
}

// Spec (U19 SS-8): an existing age file must open with the passphrase;
// a wrong one stops the switch and the file stays as it was.
func TestSwitchToAgeWrongPassphrase(t *testing.T) {
	from, dir := switchFrom(t, "keyring")
	a := newAgeFile(filepath.Join(dir, AgeFile), 10)
	if err := a.Unlock(pass); err != nil {
		t.Fatal(err)
	}
	if err := a.Set("other", "kept"); err != nil {
		t.Fatal(err)
	}
	a.Lock()
	before := readFile(t, filepath.Join(dir, AgeFile))
	_, err := Switch(from, SwitchOptions{RemovePlain: true, Options: switchOpts(dir), To: "age", Pass: []byte("wrong pass phrase")})
	if !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("err = %v", err)
	}
	if got := readFile(t, filepath.Join(dir, AgeFile)); got != before {
		t.Fatal("the age file changed")
	}
	if !keyringHas("ai.key.openai") {
		t.Fatal("the old keyring items were deleted")
	}
}

// Spec (U19 SS-8): a failed write to the age file undoes the switch.
func TestSwitchAgeWriteFails(t *testing.T) {
	from, dir := switchFrom(t, "keyring")
	idxBefore := readFile(t, filepath.Join(dir, IndexFile))
	calls := 0
	restore := setRename(func(a, b string) error {
		// The first write makes the empty file at unlock; the second,
		// with every value, fails.
		if calls++; calls == 1 {
			return os.Rename(a, b)
		}
		return errors.New("disk full")
	})
	_, err := Switch(from, SwitchOptions{RemovePlain: true, Options: switchOpts(dir), To: "age", Pass: []byte(pass)})
	restore()
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("err = %v", err)
	}
	gone(t, filepath.Join(dir, AgeFile))
	if got := readFile(t, filepath.Join(dir, IndexFile)); got != idxBefore {
		t.Fatal("the index changed")
	}
	if !keyringHas("ai.key.openai") {
		t.Fatal("the old keyring items were deleted")
	}
}

// Spec (U19 SS-8): when the index can not be written after the copy, the
// items written to the keyring are deleted again.
func TestSwitchIndexWriteFailsUndoesKeyring(t *testing.T) {
	from, dir := switchFrom(t, "age")
	t.Cleanup(setSwitchVerify(func(Backend, map[string]string) error {
		renameFn = func(string, string) error { return errors.New("index is read-only") }
		return nil
	}))
	t.Cleanup(setRename(os.Rename))
	_, err := Switch(from, SwitchOptions{RemovePlain: true, Options: switchOpts(dir), To: "keyring"})
	if err == nil || !strings.Contains(err.Error(), "index is read-only") {
		t.Fatalf("err = %v", err)
	}
	for n := range switchValues {
		if keyringHas(n) {
			t.Errorf("keyring still holds %s after the undo", n)
		}
	}
	if v, _, err := from.Get("ai.key.openai"); v != "sk-SWITCH-SECRET-1" || err != nil {
		t.Fatalf("old store Get = %q %v", v, err)
	}
}

// Spec (U19 SS-8): a failure removes an index the switch itself made,
// and puts back a plain file that was there before.
func TestSwitchFailureRestoresFiles(t *testing.T) {
	from, dir := switchFrom(t, "age")
	o := switchOpts(dir)
	if err := os.MkdirAll(filepath.Dir(o.PlainPath), 0o700); err != nil {
		t.Fatal(err)
	}
	const old = `{"other":"field"}`
	if err := os.WriteFile(o.PlainPath, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, IndexFile)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(setSwitchVerify(func(Backend, map[string]string) error { return errors.New("check failed") }))
	if _, err := Switch(from, SwitchOptions{RemovePlain: true, Options: o, To: "plain"}); err == nil {
		t.Fatal("Switch succeeded")
	}
	gone(t, filepath.Join(dir, IndexFile))
	if got := readFile(t, o.PlainPath); got != old {
		t.Fatalf("the old plain file was not put back: %q", got)
	}
}

// Spec (U19 SS-8): old copies that can not be removed after the switch
// are reported, for the plain file and for keyring items.
func TestSwitchOldCopiesNotRemoved(t *testing.T) {
	t.Run("plain", func(t *testing.T) {
		from, dir := switchFrom(t, "plain")
		t.Cleanup(setRemove(func(string) error { return errors.New("busy") }))
		s, err := Switch(from, SwitchOptions{RemovePlain: true, Options: switchOpts(dir), To: "age", Pass: []byte(pass)})
		if !errors.Is(err, ErrOldNotRemoved) || s == nil || s.Backend() != "age" {
			t.Fatalf("Switch = %v, %v", s, err)
		}
		noValueIn(t, err)
	})
	t.Run("keyring", func(t *testing.T) {
		from, dir := switchFrom(t, "keyring")
		s, err := Switch(from, SwitchOptions{RemovePlain: true, Options: switchOpts(dir), To: "plain",
			Commit: func(Store) error {
				keyring.MockInitWithError(errors.New("locked by the desktop"))
				return nil
			}})
		keyring.MockInit()
		if !errors.Is(err, ErrOldNotRemoved) || s == nil || !strings.Contains(err.Error(), "locked by the desktop") {
			t.Fatalf("Switch = %v, %v", s, err)
		}
		noValueIn(t, err)
		if v, ok, _ := s.Get("ai.key.openai"); v != "sk-SWITCH-SECRET-1" || !ok {
			t.Fatalf("new store Get = %q %v", v, ok)
		}
	})
}

// Spec (U19 SS-8): the read-back check names the secret that is wrong,
// never its value.
func TestVerifyBackendNamesOnly(t *testing.T) {
	want := map[string]string{"ai.key.openai": canary}
	cases := map[string]struct {
		prep func(*fakeBackend)
		msg  string
	}{
		"read error": {func(f *fakeBackend) { f.getErr = errors.New("io") }, "check ai.key.openai: io"},
		"missing":    {func(f *fakeBackend) { f.missing = map[string]bool{"ai.key.openai": true} }, "missing after the copy"},
		"differs":    {func(f *fakeBackend) { f.differ = map[string]bool{"ai.key.openai": true} }, "differs after the copy"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFakeBackend()
			f.data["ai.key.openai"] = canary
			c.prep(f)
			err := verifyBackend(f, want)
			if err == nil || !strings.Contains(err.Error(), c.msg) {
				t.Fatalf("err = %v, want %q", err, c.msg)
			}
			noValueIn(t, err)
		})
	}
	f := newFakeBackend()
	f.data["ai.key.openai"] = canary
	if err := verifyBackend(f, want); err != nil {
		t.Fatalf("a good copy fails the check: %v", err)
	}
}

// Spec (U19 SS-8): a plain file with a field the switch can not move
// stops the switch, so nothing is lost when the old file is removed.
func TestSwitchPlainUnknownField(t *testing.T) {
	from, dir := switchFrom(t, "plain")
	o := switchOpts(dir)
	b := readFile(t, o.PlainPath)
	b = strings.Replace(b, "{", `{"weird_field":1,`, 1)
	if err := os.WriteFile(o.PlainPath, []byte(b), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Switch(from, SwitchOptions{RemovePlain: true, Options: o, To: "age", Pass: []byte(pass)})
	if err == nil || !strings.Contains(err.Error(), `unknown field "weird_field"`) {
		t.Fatalf("err = %v", err)
	}
	noValueIn(t, err)
	if got := readFile(t, o.PlainPath); got != b {
		t.Fatal("the plain file changed")
	}
}
