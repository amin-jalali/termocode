package dbclient

import (
	"testing"
	"time"
)

// DBX-21: a connection may sit in a folder ("team/prod") and carry a
// colour from a fixed list; the folder is cleaned, an unknown colour is
// dropped with a warning, and Folders lists the folders in order.
func TestConnFolderAndColor(t *testing.T) {
	src := []byte(`{"connections":[
		{"id":"a","driver":"sqlite","path":"a.db","folder":" /team//prod/ ","color":"Red"},
		{"id":"b","driver":"sqlite","path":"b.db","folder":"local","color":"pink-ish"},
		{"id":"c","driver":"sqlite","path":"c.db"}
	]}`)
	cs, err := ParseConfig(src, SourceUser, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cs[0].Folder != "team/prod" || cs[0].Color != "red" || cs[0].Warning != "" {
		t.Errorf("a: %q %q %q", cs[0].Folder, cs[0].Color, cs[0].Warning)
	}
	if cs[1].Color != "" || cs[1].Warning == "" {
		t.Errorf("b: colour %q warning %q", cs[1].Color, cs[1].Warning)
	}
	if cs[2].Folder != "" || cs[2].Color != "" {
		t.Errorf("c: %+v", cs[2])
	}
	got := Folders(cs)
	if len(got) != 2 || got[0] != "local" || got[1] != "team/prod" {
		t.Errorf("Folders = %v", got)
	}
	if len(ConnColors()) < 6 {
		t.Errorf("colours %v", ConnColors())
	}
}

// DBX-21: auto-reconnect waits 1 s, 2 s, 4 s … up to 30 s.
func TestReconnectDelay(t *testing.T) {
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second}
	for i, w := range want {
		if got := ReconnectDelay(i); got != w {
			t.Errorf("ReconnectDelay(%d) = %v, want %v", i, got, w)
		}
	}
	if got := ReconnectDelay(-3); got != time.Second {
		t.Errorf("negative attempt: %v", got)
	}
	if got := ReconnectDelay(1000); got != 30*time.Second {
		t.Errorf("huge attempt: %v", got)
	}
}
