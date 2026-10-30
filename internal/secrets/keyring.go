package secrets

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"runtime"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/amin-jalali/termocode/internal/crash"
)

const keyringService = "termocode"

type keyringBackend struct {
	set func(string, string, string) error
	get func(string, string) (string, error)
	del func(string, string) error
}

func newKeyringBackend(set func(string, string, string) error, get func(string, string) (string, error), del func(string, string) error) *keyringBackend {
	return &keyringBackend{set: set, get: get, del: del}
}

func (k *keyringBackend) Name() string        { return "keyring" }
func (k *keyringBackend) Locked() bool        { return false }
func (k *keyringBackend) Unlock(string) error { return nil }
func (k *keyringBackend) Lock()               {}
func (k *keyringBackend) Get(name string) (string, bool, error) {
	v, err := k.get(keyringService, name)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", false, nil
	}
	if err != nil {
		notifyKeyringFail() // failures.go: reachable at setup, not here
	}
	return v, err == nil, err
}
func (k *keyringBackend) Set(name, value string) error { return k.set(keyringService, name, value) }
func (k *keyringBackend) Delete(name string) error {
	err := k.del(keyringService, name)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

// noKeyringEnv, when set, keeps the OS keyring out entirely (test
// sandboxes: a keychain that never answers costs each probe its full
// timeout, and a test must not touch the real one).
const noKeyringEnv = "TERMOCODE_NO_KEYRING"

// ProbeKeyring checks that the OS keyring can store and retrieve an item.
// It never waits longer than two seconds and skips noninteractive contexts.
func ProbeKeyring(getenv func(string) string, overSSH bool) bool {
	goos := runtime.GOOS
	if getenv == nil {
		getenv = os.Getenv
	}
	if goos == "android" || (goos == "linux" && getenv != nil && getenv("DBUS_SESSION_BUS_ADDRESS") == "") || (!overSSH && getenv != nil && getenv("SSH_CONNECTION") != "") || getenv(noKeyringEnv) != "" {
		return false
	}
	return probeKeyringWith(time.Second*2, keyring.Set, keyring.Get, keyring.Delete)
}

// KeyringSkipReason says why ProbeKeyring does not even try the OS
// keyring here ("no D-Bus session", "over SSH", "not on Android"), or ""
// when it would try.
func KeyringSkipReason(getenv func(string) string, overSSH bool) string {
	if getenv == nil {
		getenv = os.Getenv
	}
	switch {
	case getenv(noKeyringEnv) != "":
		return "turned off (" + noKeyringEnv + ")"
	case runtime.GOOS == "android":
		return "not on Android"
	case runtime.GOOS == "linux" && getenv("DBUS_SESSION_BUS_ADDRESS") == "":
		return "no D-Bus session"
	case !overSSH && getenv("SSH_CONNECTION") != "":
		return "over SSH"
	}
	return ""
}

func probeKeyringWith(timeout time.Duration, set func(string, string, string) error, get func(string, string) (string, error), del func(string, string) error) bool {
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return false
	}
	name := "probe-" + hex.EncodeToString(nonce[:])
	done := make(chan bool, 1)
	crash.Go("the keyring", func() {
		ok := false
		defer func() { done <- ok }()
		if set(keyringService, name, name) != nil {
			return
		}
		got, err := get(keyringService, name)
		_ = del(keyringService, name)
		ok = err == nil && got == name
	})
	select {
	case ok := <-done:
		return ok
	case <-time.After(timeout):
		return false
	}
}
