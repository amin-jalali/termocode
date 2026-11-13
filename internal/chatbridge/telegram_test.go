package chatbridge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const tgToken = "123456:AAtest-secret-token"

// fakeTelegram is a Bot API server: updates to hand out, and the
// messages the bot sent.
type fakeTelegram struct {
	mu      sync.Mutex
	updates []map[string]any
	sent    []map[string]any
	offsets []string
	status  int // reply status of every call when set
}

func (f *fakeTelegram) server(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if !strings.HasPrefix(r.URL.Path, "/bot"+tgToken+"/") {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"ok":false,"error_code":404,"description":"Not Found"}`)
			return
		}
		if f.status != 0 {
			w.WriteHeader(f.status)
			_, _ = io.WriteString(w, `{"ok":false,"error_code":401,"description":"Unauthorized"}`)
			return
		}
		method := strings.TrimPrefix(r.URL.Path, "/bot"+tgToken+"/")
		switch method {
		case "getMe":
			_, _ = io.WriteString(w, `{"ok":true,"result":{"id":99,"is_bot":true,"username":"tc_bot"}}`)
		case "getUpdates":
			f.offsets = append(f.offsets, r.URL.Query().Get("offset"))
			var keep []map[string]any
			off := r.URL.Query().Get("offset")
			for _, u := range f.updates {
				id := int64(u["update_id"].(int))
				neg := strings.HasPrefix(off, "-")
				if (neg && id == lastID(f.updates)) || (!neg && (off == "" || id >= atoi(off))) {
					keep = append(keep, u)
				}
			}
			b, _ := json.Marshal(map[string]any{"ok": true, "result": keep})
			_, _ = w.Write(b)
		case "sendMessage":
			var m map[string]any
			_ = json.NewDecoder(r.Body).Decode(&m)
			f.sent = append(f.sent, m)
			_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":1}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func atoi(s string) int64 {
	var n int64
	neg := strings.HasPrefix(s, "-")
	for _, c := range strings.TrimPrefix(s, "-") {
		n = n*10 + int64(c-'0')
	}
	if neg {
		return -n
	}
	return n
}

func lastID(us []map[string]any) int64 {
	var m int64
	for _, u := range us {
		if id := int64(u["update_id"].(int)); id > m {
			m = id
		}
	}
	return m
}

func tgUpdate(id int, user int, text string) map[string]any {
	return map[string]any{"update_id": id, "message": map[string]any{
		"message_id": id, "text": text,
		"from": map[string]any{"id": user, "is_bot": false, "username": "amin"},
		"chat": map[string]any{"id": user, "type": "private"},
	}}
}

func TestTelegramStartSkipsTheBacklog(t *testing.T) {
	f := &fakeTelegram{updates: []map[string]any{tgUpdate(10, 7, "old one"), tgUpdate(11, 7, "old two")}}
	tg := &Telegram{Token: tgToken, Base: f.server(t).URL, Wait: time.Millisecond}
	if err := tg.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	msgs, err := tg.Poll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("messages sent while the bridge was off must not run: %+v", msgs)
	}
	f.mu.Lock()
	f.updates = append(f.updates, tgUpdate(12, 7, "new"))
	f.mu.Unlock()
	msgs, err = tg.Poll(context.Background())
	if err != nil || len(msgs) != 1 || msgs[0].Text != "new" || msgs[0].User != "7" || msgs[0].Chat != "7" {
		t.Fatalf("poll after start: %+v, %v", msgs, err)
	}
	msgs, _ = tg.Poll(context.Background())
	if len(msgs) != 0 {
		t.Fatalf("an update is handed out once: %+v", msgs)
	}
}

func TestTelegramPollSkipsBotsAndNonText(t *testing.T) {
	f := &fakeTelegram{}
	tg := &Telegram{Token: tgToken, Base: f.server(t).URL, Wait: time.Millisecond}
	if err := tg.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	bot := tgUpdate(1, 8, "from a bot")
	bot["message"].(map[string]any)["from"].(map[string]any)["is_bot"] = true
	photo := tgUpdate(2, 7, "")
	f.mu.Lock()
	f.updates = []map[string]any{bot, photo, tgUpdate(3, 7, "hi")}
	f.mu.Unlock()
	msgs, err := tg.Poll(context.Background())
	if err != nil || len(msgs) != 1 || msgs[0].Text != "hi" {
		t.Fatalf("only a person's text counts: %+v, %v", msgs, err)
	}
}

func TestTelegramSendSplitsLongText(t *testing.T) {
	f := &fakeTelegram{}
	tg := &Telegram{Token: tgToken, Base: f.server(t).URL}
	long := strings.Repeat("a", TelegramMax+10)
	if err := tg.Send(context.Background(), "7", long); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) != 2 || f.sent[0]["chat_id"] != "7" {
		t.Fatalf("sent: %d parts %+v", len(f.sent), f.sent)
	}
	if n := len([]rune(f.sent[0]["text"].(string))); n > TelegramMax {
		t.Fatalf("part of %d runes, max %d", n, TelegramMax)
	}
}

func TestTelegramErrorsNeverShowTheToken(t *testing.T) {
	f := &fakeTelegram{status: http.StatusUnauthorized}
	tg := &Telegram{Token: tgToken, Base: f.server(t).URL}
	err := tg.Start(context.Background())
	if err == nil || !IsUnauthorized(err) {
		t.Fatalf("a bad token: %v", err)
	}
	if strings.Contains(err.Error(), tgToken) || strings.Contains(err.Error(), "AAtest") {
		t.Fatalf("the token leaked: %v", err)
	}
	// A connection error carries the URL; the token is cut from it too.
	dead := &Telegram{Token: tgToken, Base: "http://127.0.0.1:1"}
	err = dead.Send(context.Background(), "7", "x")
	if err == nil || strings.Contains(err.Error(), "AAtest") {
		t.Fatalf("connection error: %v", err)
	}
}

func TestTelegramNeedsAToken(t *testing.T) {
	if err := (&Telegram{}).Start(context.Background()); err == nil {
		t.Fatal("no token, but started")
	}
}
