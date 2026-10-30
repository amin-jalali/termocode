//go:build unix

package app

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webtotp"
)

func totpRig(t *testing.T) *accessRig {
	t.Helper()
	r := newAccessRig(t)
	writeUserConfig(t, `{"web_panel":"on","web_listen":"tailnet","secret_store":"plain"}`)
	return r
}

// Spec (WEB-48): Web: Set Up Sign-in Code… makes a seed, keeps it only in
// the secret store (web.totp) and shows it once as an otpauth QR code
// and as text for typing; a second setup asks first (Keep first).
func TestWebTotpSetup(t *testing.T) {
	r := totpRig(t)
	r.m.openWebTotpSetup()
	if !r.m.confirmOpen || r.m.confirmKind != confirmKindWebTotpShow {
		t.Fatal("no setup card")
	}
	s, err := r.m.secretStore()
	if err != nil {
		t.Fatal(err)
	}
	seed, ok, err := s.Get(webTotpSecretName)
	if err != nil || !ok || len(seed) != 32 {
		t.Fatalf("seed in the store: %v %v (%d chars)", ok, err, len(seed))
	}
	view := r.m.confirm.View()
	if !strings.Contains(view, "█") || !strings.Contains(strings.ReplaceAll(view, " ", ""), seed[:8]) {
		t.Fatalf("setup card:\n%s", view)
	}
	webAccessSelect(r.m, "close")
	r.m.openWebTotpSetup()
	if r.m.confirmKind != confirmKindWebTotpReplace || r.m.confirm.SafeID() != "keep" {
		t.Fatal("a second setup did not ask first")
	}
	webAccessSelect(r.m, "keep")
	if again, _, _ := s.Get(webTotpSecretName); again != seed {
		t.Fatal("Keep replaced the seed")
	}
}

// Spec (WEB-48): the gateway's web.totp_check is checked here against the
// seed (which never leaves): the right code passes once, a wrong or a
// replayed one is refused; no seed at all is refused too.
func TestWebTotpCheck(t *testing.T) {
	r := totpRig(t)
	ask := func(id int, code string) map[string]any {
		r.ctl.send(`{"t":"req","id":` + strconv.Itoa(id) + `,"op":"web.totp_check","args":{"code":"` + code + `","device":"dev-1a2b3c4d"}}`)
		for {
			m := r.nextMsg(r.ctl)
			if m["t"] == "res" || m["t"] == "err" {
				return m
			}
		}
	}
	if m := ask(1, "123456"); m["t"] != "err" {
		t.Fatalf("no seed: %v", m)
	}
	r.m.openWebTotpSetup()
	s, _ := r.m.secretStore()
	b32, _, _ := s.Get(webTotpSecretName)
	seed, _ := webtotp.Decode(b32)
	good := webtotp.Code(seed, time.Now())
	if m := ask(2, wrongCode(good)); m["t"] != "err" || m["code"] != "forbidden" {
		t.Fatalf("wrong code: %v", m)
	}
	if m := ask(3, good); m["t"] != "res" {
		t.Fatalf("right code: %v", m)
	}
	if m := ask(4, good); m["t"] != "err" {
		t.Fatalf("replayed code: %v", m)
	}
	for _, line := range r.ctl.drain() {
		if strings.Contains(line, b32) || strings.Contains(line, good) {
			t.Fatal("the seed or the code went back to the gateway")
		}
	}
}

func wrongCode(c string) string {
	if c == "000000" {
		return "000001"
	}
	return "000000"
}

// drain returns the lines read so far without waiting.
func (c *webClient) drain() []string {
	var out []string
	for {
		select {
		case l, ok := <-c.lines:
			if !ok {
				return out
			}
			out = append(out, l)
		default:
			return out
		}
	}
}

// Spec (3.3, ADR 0020): the hub's files never import the secret store;
// only the TOTP bridge (outside web_*.go) reads the seed.
func TestWebFilesNeverImportSecrets(t *testing.T) {
	files, _ := filepath.Glob("web_*.go")
	if len(files) < 20 {
		t.Fatalf("only %d web_*.go files", len(files))
	}
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, im := range af.Imports {
			if strings.Contains(im.Path.Value, "internal/secrets") {
				t.Errorf("%s imports %s", f, im.Path.Value)
			}
		}
	}
}

// Spec (WEB-48): web_require_totp (off by default) starts the gateway
// with --totp.
func TestWebRequireTotpFlag(t *testing.T) {
	withConfigDir(t)
	if webSettings().RequireTOTP {
		t.Fatal("TOTP on by default")
	}
	writeUserConfig(t, `{"web_require_totp":true}`)
	if !webSettings().RequireTOTP {
		t.Fatal("web_require_totp not read")
	}
}
