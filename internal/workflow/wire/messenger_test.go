package wire

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type fakeSecrets map[string]string

func (f fakeSecrets) Get(name string) (string, bool, error) {
	v, ok := f[name]
	return v, ok, nil
}

// Spec (plan §3.4, WFX-18): a message goes to the OS ("os"), to the
// browsers ("web") or to a webhook whose URL is a secret
// ("${secret:NAME}", https only, posted as JSON {"text": …}). An error
// never shows the webhook's URL. Other channels are refused clearly.
func TestMessengerDelivers(t *testing.T) {
	var osGot, webGot []string
	var posts []string
	hook := "https://hooks.example.test/" + "T0/" + "abc"
	ms := Messenger{
		OS:      func(_ context.Context, text string) error { osGot = append(osGot, text); return nil },
		Web:     func(_ context.Context, text string) error { webGot = append(webGot, text); return nil },
		Secrets: fakeSecrets{"HOOK": hook, "PLAIN": "http://hooks.example.test/x"},
		Post: func(_ context.Context, url string, body []byte) error {
			posts = append(posts, url+" "+string(body))
			return nil
		},
	}
	ctx := context.Background()
	for _, via := range []string{"os", "web", "${secret:HOOK}"} {
		if err := ms.Send(ctx, via, `wf: done · 2 items "ok"`); err != nil {
			t.Fatalf("%s: %v", via, err)
		}
	}
	if len(osGot) != 1 || len(webGot) != 1 || osGot[0] != `wf: done · 2 items "ok"` {
		t.Fatalf("os %q web %q", osGot, webGot)
	}
	want, _ := json.Marshal(map[string]string{"text": `wf: done · 2 items "ok"`})
	if len(posts) != 1 || posts[0] != hook+" "+string(want) {
		t.Fatalf("posts %q", posts)
	}
	for via, why := range map[string]string{
		"${secret:PLAIN}":   "https",
		"${secret:MISSING}": "not set",
		"${env:HOOK}":       "${secret:NAME}",
		"slack":             "MCP",
	} {
		err := ms.Send(ctx, via, "x")
		if err == nil || !strings.Contains(err.Error(), why) || strings.Contains(err.Error(), "hooks.example.test") {
			t.Errorf("%s: %v (want it to say %q, never the URL)", via, err, why)
		}
	}
	if len(posts) != 1 {
		t.Fatalf("a refused channel posted: %q", posts)
	}
	if err := (Messenger{}).Send(ctx, "os", "x"); err == nil {
		t.Error("no OS channel, no error")
	}
}
