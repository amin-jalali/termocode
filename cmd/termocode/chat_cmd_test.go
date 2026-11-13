package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// chatSeams replaces the secret store, the AI turn and the service
// addresses for one test.
func chatSeams(t *testing.T, secret map[string]string, turn func(context.Context, chatTurnOpts, string) (string, error), base string) {
	t.Helper()
	oldGet, oldSet, oldTurn, oldBase := chatSecretGet, chatSecretSet, chatAITurn, chatBase
	t.Cleanup(func() { chatSecretGet, chatSecretSet, chatAITurn, chatBase = oldGet, oldSet, oldTurn, oldBase })
	var mu sync.Mutex
	chatSecretGet = func(name string) (string, bool, error) {
		mu.Lock()
		defer mu.Unlock()
		v, ok := secret[name]
		return v, ok, nil
	}
	chatSecretSet = func(name, v string) error {
		mu.Lock()
		defer mu.Unlock()
		secret[name] = v
		return nil
	}
	chatAITurn = turn
	chatBase = map[string]string{"telegram": base, "discord": base}
}

func chatRun(t *testing.T, ctx context.Context, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errw bytes.Buffer
	code := runChatIn(ctx, args, strings.NewReader(stdin), &out, &errw)
	return code, out.String(), errw.String()
}

func TestChatUsage(t *testing.T) {
	chatSeams(t, map[string]string{}, nil, "")
	if code, _, e := chatRun(t, context.Background(), ""); code != 2 || !strings.Contains(e, "termocode chat telegram") {
		t.Fatalf("no args: %d %q", code, e)
	}
	if code, o, _ := chatRun(t, context.Background(), "", "--help"); code != 0 || !strings.Contains(o, "--allow") {
		t.Fatalf("help: %d %q", code, o)
	}
	if code, _, e := chatRun(t, context.Background(), "", "slack", "--allow", "1"); code != 2 || !strings.Contains(e, "telegram or discord") {
		t.Fatalf("unknown service: %d %q", code, e)
	}
}

func TestChatNeedsAllowAndChannel(t *testing.T) {
	chatSeams(t, map[string]string{"chat.telegram.token": "t", "chat.discord.token": "d"}, nil, "")
	if code, _, e := chatRun(t, context.Background(), "", "telegram"); code != 2 || !strings.Contains(e, "--allow") {
		t.Fatalf("no --allow: %d %q", code, e)
	}
	if code, _, e := chatRun(t, context.Background(), "", "discord", "--allow", "7"); code != 2 || !strings.Contains(e, "--channel") {
		t.Fatalf("discord without --channel: %d %q", code, e)
	}
	if code, _, e := chatRun(t, context.Background(), "", "telegram", "--allow", "7", "--tools", "all"); code != 2 || !strings.Contains(e, "--tools") {
		t.Fatalf("a chat never gets the mutating tools: %d %q", code, e)
	}
}

func TestChatWithoutTokenSaysHowToSetIt(t *testing.T) {
	chatSeams(t, map[string]string{}, nil, "")
	code, _, e := chatRun(t, context.Background(), "", "telegram", "--allow", "7")
	if code != 1 || !strings.Contains(e, "termocode chat token telegram") {
		t.Fatalf("no token: %d %q", code, e)
	}
}

func TestChatTokenSavesToTheSecretStore(t *testing.T) {
	sec := map[string]string{}
	chatSeams(t, sec, nil, "")
	code, o, e := chatRun(t, context.Background(), "123:ABCsecret\n", "token", "telegram")
	if code != 0 || sec["chat.telegram.token"] != "123:ABCsecret" {
		t.Fatalf("token: %d %q %v", code, e, sec)
	}
	if strings.Contains(o+e, "ABCsecret") {
		t.Fatalf("the token was printed: %q %q", o, e)
	}
	if code, _, _ := chatRun(t, context.Background(), "\n", "token", "discord"); code == 0 {
		t.Fatal("an empty token was saved")
	}
	chatSecretSet = func(string, string) error { return errors.New("the secret store is locked") }
	if code, _, e := chatRun(t, context.Background(), "x\n", "token", "discord"); code != 1 || !strings.Contains(e, "locked") {
		t.Fatalf("store error: %d %q", code, e)
	}
}

// TestChatTelegramAnswersWithAnAITurn runs the bridge against a fake
// Bot API: one question from the allowed user is one read-only AI turn
// in --dir, and its answer goes back to that chat.
func TestChatTelegramAnswersWithAnAITurn(t *testing.T) {
	const token = "42:tok"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var mu sync.Mutex
	var sent []map[string]any
	polls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if !strings.HasPrefix(r.URL.Path, "/bot"+token+"/") {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"ok":false,"description":"Unauthorized"}`)
			return
		}
		switch strings.TrimPrefix(r.URL.Path, "/bot"+token+"/") {
		case "getMe":
			_, _ = io.WriteString(w, `{"ok":true,"result":{"id":1}}`)
		case "getUpdates":
			if r.URL.Query().Get("offset") == "-1" {
				_, _ = io.WriteString(w, `{"ok":true,"result":[]}`)
				return
			}
			polls++
			if polls == 1 {
				_, _ = io.WriteString(w, `{"ok":true,"result":[{"update_id":5,"message":{"text":"what is in main.go?","from":{"id":7},"chat":{"id":7}}}]}`)
				return
			}
			_, _ = io.WriteString(w, `{"ok":true,"result":[]}`)
		case "sendMessage":
			var m map[string]any
			_ = json.NewDecoder(r.Body).Decode(&m)
			sent = append(sent, m)
			cancel()
			_, _ = io.WriteString(w, `{"ok":true,"result":{}}`)
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	var got chatTurnOpts
	var asked string
	chatSeams(t, map[string]string{"chat.telegram.token": token}, func(_ context.Context, o chatTurnOpts, q string) (string, error) {
		got, asked = o, q
		return "main.go starts the app.", nil
	}, srv.URL)
	code, _, e := chatRun(t, ctx, "", "telegram", "--allow", "7,8", "--dir", dir)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, e)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 1 || sent[0]["text"] != "main.go starts the app." || sent[0]["chat_id"] != "7" {
		t.Fatalf("sent: %+v", sent)
	}
	if asked != "what is in main.go?" || got.tools != "read" || filepath.Clean(got.dir) != filepath.Clean(dir) {
		t.Fatalf("turn: %+v %q", got, asked)
	}
	if strings.Contains(e, token) {
		t.Fatalf("the token is in the log: %q", e)
	}
}

func TestChatAITurnRunsTermocodeAIReadOnly(t *testing.T) {
	old := chatRunAI
	t.Cleanup(func() { chatRunAI = old })
	var args []string
	chatRunAI = func(_ context.Context, a []string, _ io.Reader, out, errw io.Writer) int {
		args = a
		io.WriteString(out, "the answer\n")
		return 0
	}
	ans, err := defaultChatAITurn(context.Background(), chatTurnOpts{dir: "/w", tools: "read", model: "m1"}, "q?")
	if err != nil || ans != "the answer" {
		t.Fatalf("%q %v", ans, err)
	}
	if strings.Join(args, " ") != "-x q? --output text --dir /w --tools read --model m1" {
		t.Fatalf("args: %q", args)
	}
	chatRunAI = func(_ context.Context, _ []string, _ io.Reader, _, errw io.Writer) int {
		io.WriteString(errw, "» read main.go\ntermocode ai: AI is not configured\n")
		return 3
	}
	if _, err := defaultChatAITurn(context.Background(), chatTurnOpts{dir: "/w", tools: "read"}, "q"); err == nil || err.Error() != "AI is not configured" {
		t.Fatalf("failed turn: %v", err)
	}
}
