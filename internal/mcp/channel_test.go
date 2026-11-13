package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/amin-jalali/termocode/internal/extproc"
)

// PR-37: a server may push a message with notifications/claude/channel
// ({content, meta}). The client hands it to Options.Channel.
func TestClientPassesAChannelMessage(t *testing.T) {
	var gotContent string
	var gotMeta map[string]string
	c := &Client{opts: Options{Channel: func(content string, meta map[string]string) { gotContent, gotMeta = content, meta }}}
	c.handle(extproc.Message{Method: "notifications/claude/channel",
		Params: json.RawMessage(`{"content":"build failed on main","meta":{"severity":"high","n":3}}`)}, nil)
	if gotContent != "build failed on main" || gotMeta["severity"] != "high" || gotMeta["n"] != "3" {
		t.Fatalf("channel got %q %v", gotContent, gotMeta)
	}
	// With no hook the notification is dropped, and a bad one too.
	d := &Client{}
	d.handle(extproc.Message{Method: "notifications/claude/channel", Params: json.RawMessage(`{"content":"x"}`)}, nil)
	c.handle(extproc.Message{Method: "notifications/claude/channel", Params: json.RawMessage(`not json`)}, nil)
	if gotContent != "build failed on main" {
		t.Fatal("a broken message changed the last one")
	}
}

// mcp_channels lists the servers whose channel messages are shown (none by default).
func TestLoadSettingsChannels(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	if got := LoadSettings(p).Channels; len(got) != 0 {
		t.Fatalf("default channels %v", got)
	}
	if err := os.WriteFile(p, []byte(`{"mcp_channels":["ci","chat"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := LoadSettings(p).Channels; !reflect.DeepEqual(got, []string{"ci", "chat"}) {
		t.Fatalf("channels %v", got)
	}
}
