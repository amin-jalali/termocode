//go:build unix

package webgw

import (
	"net/http"
	"strings"
	"testing"
)

// FuzzAuthJSON sends one fuzzed POST /auth ("$S" in the body stands for
// the real launch secret) through the guard. Invariants: no panic; a known
// status; never a cookie; the secret never echoed; 200 only when the body
// held the secret, with a token that works; and a refused request never
// uses up the secret, so the clean request after it still gets in.
func FuzzAuthJSON(f *testing.F) {
	for _, seed := range []struct{ body, ct string }{
		{`{"secret":"$S"}`, "application/json"},
		{`{"secret":"$S"}`, "application/json; charset=utf-8"},
		{`{"secret":"$S"}`, "text/plain"},
		{`{"secret":"$S","x":1}`, "application/json"},
		{`{"SECRET":"$S"}`, "application/json"},
		{`{"secret":"$S"}{"secret":"$S"}`, "application/json"},
		{`{"secret":"$S"}`, "application/json"},
		{`[{"secret":"$S"}]`, "application/json"},
		{`{"secret":["$S"]}`, "application/json"},
		{`{"secret":"$S\u0000"}`, "application/json"},
		{`{"secret":"$S`, "application/json"},
		{"\xff\xfe{\"secret\":\"$S\"}", "application/json"},
		{`secret=$S`, "application/x-www-form-urlencoded"},
		{``, ""},
	} {
		f.Add(seed.body, seed.ct)
	}
	f.Fuzz(func(t *testing.T, tmpl, ct string) {
		a, ts, _, plain := newAuth(t)
		body := strings.ReplaceAll(tmpl, "$S", plain)
		r := authReq(body)
		r.Header.Set("Content-Type", ct)
		w := do(a, r)
		switch w.Code {
		case http.StatusOK, http.StatusBadRequest, http.StatusUnauthorized,
			http.StatusRequestEntityTooLarge, http.StatusUnsupportedMediaType:
		default:
			t.Fatalf("status %d", w.Code)
		}
		if len(w.Header().Values("Set-Cookie")) > 0 {
			t.Fatal("cookie set")
		}
		if strings.Contains(w.Body.String(), plain) {
			t.Fatal("the secret was echoed")
		}
		if w.Code == http.StatusOK {
			if !strings.Contains(body, plain) {
				t.Fatalf("200 for a body without the secret: %q", body)
			}
			var tok string
			if i := strings.Index(w.Body.String(), `"token":"`); i >= 0 {
				rest := w.Body.String()[i+len(`"token":"`):]
				tok = rest[:strings.IndexByte(rest, '"')]
			}
			if _, err := ts.Check(tok, t0); err != nil {
				t.Fatalf("issued token refused: %v", err)
			}
			if w := do(a, authReq(secretBody(plain))); w.Code == http.StatusOK {
				t.Fatal("the secret redeemed twice")
			}
			return
		}
		if w := do(a, authReq(secretBody(plain))); w.Code != http.StatusOK {
			t.Fatalf("after a refused %q (%q, %d) the secret no longer works: %d", body, ct, w.Code, w.Code)
		}
	})
}
