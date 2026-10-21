package secrets

import (
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

func TestKeyringBackendRoundTrip(t *testing.T) {
	values := map[string]string{}
	k := newKeyringBackend(func(service, name, value string) error {
		if service != keyringService {
			t.Fatalf("service = %q", service)
		}
		values[name] = value
		return nil
	}, func(service, name string) (string, error) {
		v, ok := values[name]
		if !ok {
			return "", errors.New("missing")
		}
		return v, nil
	}, func(service, name string) error { delete(values, name); return nil })
	if err := k.Set("ai.key.openai", "never-log-this"); err != nil {
		t.Fatal(err)
	}
	v, ok, err := k.Get("ai.key.openai")
	if err != nil || !ok || v != "never-log-this" {
		t.Fatalf("Get = %q %v %v", v, ok, err)
	}
	if err := k.Delete("ai.key.openai"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := k.Get("ai.key.openai"); ok {
		t.Fatal("secret remains")
	}
}

func TestOpenWithMockKeyring(t *testing.T) {
	keyring.MockInit()
	s, err := Open(Options{Dir: t.TempDir(), Backend: "keyring", KeyringProbe: func() bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set("ai.key.openai", "secret-value"); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Get("ai.key.openai")
	if err != nil || !ok || got != "secret-value" {
		t.Fatalf("Get = %q %v %v", got, ok, err)
	}
}

func TestProbeSkipsKeyringOverSSHByDefault(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("D-Bus session check is Linux-specific")
	}
	keyring.MockInit()
	env := func(name string) string {
		if name == "DBUS_SESSION_BUS_ADDRESS" {
			return "unix:path=/tmp/bus"
		}
		if name == "SSH_CONNECTION" {
			return "client"
		}
		return ""
	}
	if ProbeKeyring(env, false) {
		t.Fatal("probe ran under SSH by default")
	}
	if !ProbeKeyring(env, true) {
		t.Fatal("allowed SSH keyring probe failed")
	}
}

func TestProbeKeyringTimeout(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	blocked := func(string, string, string) error { close(started); <-release; return errors.New("unavailable") }
	if probeKeyringWith(10*time.Millisecond, blocked, nil, nil) {
		t.Fatal("blocked keyring answered")
	}
	<-started
	close(release)
}

func TestProbeKeyringChecksReadBack(t *testing.T) {
	if !probeKeyringWith(time.Second, func(string, string, string) error { return nil }, func(_, name string) (string, error) { return name, nil }, func(string, string) error { return nil }) {
		t.Fatal("working keyring did not answer")
	}
	if probeKeyringWith(time.Second, func(string, string, string) error { return nil }, func(_, _ string) (string, error) { return "wrong", nil }, func(string, string) error { return nil }) {
		t.Fatal("mismatched keyring answered")
	}
}

func TestKeyringSkipReason(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	bus := map[string]string{"DBUS_SESSION_BUS_ADDRESS": "unix:path=/run/user/1/bus"}
	ssh := map[string]string{"DBUS_SESSION_BUS_ADDRESS": "unix:path=/run/user/1/bus", "SSH_CONNECTION": "1 2 3 4"}
	switch runtime.GOOS {
	case "linux":
		if got := KeyringSkipReason(env(nil), false); got != "no D-Bus session" {
			t.Errorf("no bus: %q", got)
		}
	case "android":
		if got := KeyringSkipReason(env(bus), false); got != "not on Android" {
			t.Errorf("android: %q", got)
		}
		return
	}
	if got := KeyringSkipReason(env(bus), false); got != "" {
		t.Errorf("desktop: %q", got)
	}
	if got := KeyringSkipReason(env(ssh), false); got != "over SSH" {
		t.Errorf("ssh: %q", got)
	}
	if got := KeyringSkipReason(env(ssh), true); got != "" {
		t.Errorf("ssh allowed: %q", got)
	}
}
