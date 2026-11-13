package chatbridge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

const dcToken = "discord-secret-token"

// fakeDiscord is the REST API: messages per channel (oldest first) and
// the messages the bot posted.
type fakeDiscord struct {
	mu     sync.Mutex
	msgs   map[string][]map[string]any
	posted []map[string]any
	auth   []string
	ua     []string
}

func (f *fakeDiscord) server(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		f.ua = append(f.ua, r.Header.Get("User-Agent"))
		if r.Header.Get("Authorization") != "Bot "+dcToken {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"message":"401: Unauthorized","code":0}`)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/api/v10")
		if path == "/users/@me" {
			_, _ = io.WriteString(w, `{"id":"500","username":"tc_bot","bot":true}`)
			return
		}
		rest, ok := strings.CutPrefix(path, "/channels/")
		ch, tail, _ := strings.Cut(rest, "/")
		if !ok || tail != "messages" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method == http.MethodPost {
			var m map[string]any
			_ = json.NewDecoder(r.Body).Decode(&m)
			m["channel"] = ch
			f.posted = append(f.posted, m)
			_, _ = io.WriteString(w, `{"id":"1"}`)
			return
		}
		after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		var out []map[string]any
		all := f.msgs[ch]
		for i := len(all) - 1; i >= 0; i-- { // newest first, like Discord
			id, _ := strconv.ParseInt(all[i]["id"].(string), 10, 64)
			if r.URL.Query().Get("after") != "" && id <= after {
				continue
			}
			out = append(out, all[i])
			if limit > 0 && len(out) == limit {
				break
			}
		}
		b, _ := json.Marshal(out)
		_, _ = w.Write(b)
	}))
	t.Cleanup(s.Close)
	return s
}

func dcMsg(id, user, text string, bot bool) map[string]any {
	return map[string]any{"id": id, "content": text, "author": map[string]any{"id": user, "username": "u" + user, "bot": bot}}
}

func TestDiscordStartSkipsHistoryThenPollsNewMessages(t *testing.T) {
	f := &fakeDiscord{msgs: map[string][]map[string]any{"c1": {dcMsg("100", "7", "old", false)}, "c2": {}}}
	d := &Discord{Token: dcToken, Base: f.server(t).URL + "/api/v10", Channels: []string{"c1", "c2"}}
	if err := d.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	msgs, err := d.Poll(context.Background())
	if err != nil || len(msgs) != 0 {
		t.Fatalf("history must not run: %+v, %v", msgs, err)
	}
	f.mu.Lock()
	f.msgs["c1"] = append(f.msgs["c1"], dcMsg("101", "7", "first", false), dcMsg("102", "500", "own reply", true), dcMsg("103", "9", "other bot", true), dcMsg("104", "7", "second", false))
	f.msgs["c2"] = append(f.msgs["c2"], dcMsg("200", "7", "in c2", false))
	f.mu.Unlock()
	msgs, err = d.Poll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range msgs {
		got = append(got, m.Chat+":"+m.User+":"+m.Text)
	}
	if strings.Join(got, "|") != "c1:7:first|c1:7:second|c2:7:in c2" {
		t.Fatalf("oldest first, people only: %v", got)
	}
	if msgs, _ := d.Poll(context.Background()); len(msgs) != 0 {
		t.Fatalf("a message is handed out once: %+v", msgs)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !strings.HasPrefix(f.ua[0], "DiscordBot (") {
		t.Fatalf("Discord wants a DiscordBot user agent: %q", f.ua[0])
	}
}

func TestDiscordSendSplitsAndPingsNobody(t *testing.T) {
	f := &fakeDiscord{msgs: map[string][]map[string]any{}}
	d := &Discord{Token: dcToken, Base: f.server(t).URL + "/api/v10", Channels: []string{"c1"}}
	if err := d.Send(context.Background(), "c1", strings.Repeat("b", DiscordMax*2+1)); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.posted) != 3 {
		t.Fatalf("posted %d parts", len(f.posted))
	}
	am, _ := json.Marshal(f.posted[0]["allowed_mentions"])
	if string(am) != `{"parse":[]}` {
		t.Fatalf("an answer must never ping @everyone or a role: %s", am)
	}
}

func TestDiscordBadTokenIsUnauthorizedWithoutTheToken(t *testing.T) {
	f := &fakeDiscord{}
	d := &Discord{Token: "wrong-token-xyz", Base: f.server(t).URL + "/api/v10", Channels: []string{"c1"}}
	err := d.Start(context.Background())
	if err == nil || !IsUnauthorized(err) || strings.Contains(err.Error(), "wrong-token-xyz") {
		t.Fatalf("bad token: %v", err)
	}
}

func TestDiscordNeedsTokenAndChannel(t *testing.T) {
	if err := (&Discord{Channels: []string{"c"}}).Start(context.Background()); err == nil {
		t.Fatal("no token, but started")
	}
	if err := (&Discord{Token: "t"}).Start(context.Background()); err == nil {
		t.Fatal("no channel, but started")
	}
}
