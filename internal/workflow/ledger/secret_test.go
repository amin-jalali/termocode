package ledger

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Spec (WFX-6, plan §6.2): no secret ever reaches the ledger file: the
// args are only hashed, and the target, undo recipe and error text are
// redacted. A test greps the whole state dir.
func TestLedgerStoresNoSecret(t *testing.T) {
	root := t.TempDir()
	token := "sk-" + "test0123456789abcdefghijklmnop"
	bearer := "ghp" + "_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	l, _ := Open(root, "wf", "r1")
	w := Write{
		Step: "post", Item: "I-1",
		Target: "http POST https://user:" + "hunter2pass" + "@api.example.com/hook",
		Args:   map[string]any{"Authorization": "Bearer " + bearer, "key": token},
		Undo:   &Undo{Title: "DELETE with token=" + token, Args: map[string]string{"api_key": token, "auth": "Bearer " + bearer}},
	}
	_, _, err := l.Do(context.Background(), w, func(context.Context, string) (Result, error) {
		return Result{}, errors.New("401 for Authorization: Bearer " + bearer + " api_key=" + token)
	}, nil)
	if err == nil {
		t.Fatal("no error")
	}
	w.Item = "I-2"
	_, _, _ = l.Do(context.Background(), w, func(context.Context, string) (Result, error) {
		return Result{ExternalID: "id with token=" + token}, nil
	}, nil)
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, _ error) error {
		if d == nil || d.IsDir() {
			return nil
		}
		b, _ := os.ReadFile(p)
		for _, s := range []string{token, bearer, "hunter2pass"} {
			if strings.Contains(string(b), s) {
				t.Errorf("%s holds a secret %q:\n%s", p, s[:6], b)
			}
		}
		return nil
	})
	ents := l.Entries()
	if e, _, _ := l.Do(context.Background(), w, nil, nil); e.ExternalID != "" {
		ents = append(ents, e) // the returned entry is redacted too
	}
	for _, e := range ents {
		for _, s := range []string{e.Target, e.Error, e.ExternalID, e.Undo.Title} {
			if strings.Contains(s, token) || strings.Contains(s, bearer) {
				t.Errorf("entry in memory holds a secret: %q", s)
			}
		}
	}
}
