package testkit

import (
	"strings"
	"testing"
	"time"
)

// U19 SS-4: the old plain ai/credentials.json is offered a move to a
// safer store once at start (a workspace opens directly here, so as a
// card over it). The card counts kinds and never shows a value.

const tkMigrateKey = "sk-tk-ss4-never-on-screen"

// Spec: a plain file with secrets → the card; Keep plain → secret_store
// plain, and the card does not come back after a restart.
func TestSecretsMigrateCardKeepPlainBehavior(t *testing.T) {
	d := New(t, Options{Config: map[string]string{
		"ai/credentials.json": `{"keys":{"openai":"` + tkMigrateKey + `"},"mcp_secrets":{"PAT":"ghp_tk_ss4_value"}}`,
	}})
	if !d.WaitForText("Move your secrets", 3*time.Second) {
		t.Fatalf("no migration card:\n%s", screen(d))
	}
	sc := screen(d)
	for _, want := range []string{"1 AI key · 1 MCP secret", "no D-Bus session", "Encrypted file", "Keep plain"} {
		if !strings.Contains(sc, want) {
			t.Errorf("card misses %q:\n%s", want, sc)
		}
	}
	for _, v := range []string{tkMigrateKey, "ghp_tk_ss4_value"} {
		if strings.Contains(sc, v) {
			t.Fatalf("card shows a value:\n%s", sc)
		}
	}
	d.Key("right")
	d.Key("enter")
	if !d.WaitForText("Secrets stay in the plain file", 2*time.Second) {
		t.Fatalf("no toast:\n%s", screen(d))
	}
	if v := readConfig(t)["secret_store"]; v != "plain" {
		t.Fatalf("secret_store = %v", v)
	}
	d.Restart()
	d.Settle(500 * time.Millisecond)
	if strings.Contains(screen(d), "Move your secrets") {
		t.Fatalf("card back after Keep plain:\n%s", screen(d))
	}
}

// A fresh sandbox (no credentials.json) never shows the card.
func TestSecretsMigrateFreshSandboxNoCardBehavior(t *testing.T) {
	d := New(t, Options{})
	d.Settle(500 * time.Millisecond)
	if strings.Contains(screen(d), "Move your secrets") {
		t.Fatalf("card in a fresh sandbox:\n%s", screen(d))
	}
}
