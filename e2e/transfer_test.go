//go:build e2e

package e2e

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// A kitty file transfer (OSC 5113) answered by this test as kitty would:
// the commands are read from the real output of termocode and the
// answers typed into its terminal input, where transfer.Input must take
// them out of the key stream.

var kittyCmdRe = regexp.MustCompile("\x1b\\]5113;([^\x1b\x07]*)(?:\x1b\\\\|\x07)")

type kittyPeer struct {
	tm    *Term
	seen  int
	data  bytes.Buffer
	zip   bool
	dest  string
	done  bool
	steps []string
}

func kv(s string) map[string]string {
	m := map[string]string{}
	for _, p := range strings.Split(s, ";") {
		k, v, _ := strings.Cut(p, "=")
		m[k] = v
	}
	return m
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func (k *kittyPeer) reply(id string, extra string) {
	k.tm.Send("\x1b]5113;ac=status;id=" + id + extra + "\x1b\\")
}

// poll answers every new command; it reports whether finish was seen.
func (k *kittyPeer) poll(t *testing.T) bool {
	all := kittyCmdRe.FindAllStringSubmatch(k.tm.rawOutput(), -1)
	for _, m := range all[k.seen:] {
		c := kv(m[1])
		k.steps = append(k.steps, c["ac"])
		id := c["id"]
		switch c["ac"] {
		case "send":
			k.reply(id, ";st="+b64("OK"))
		case "file":
			n, _ := base64.StdEncoding.DecodeString(c["n"])
			k.dest, k.zip = string(n), c["zip"] == "zlib"
			k.reply(id, ";fid="+c["fid"]+";st="+b64("STARTED"))
		case "data", "end_data":
			d, err := base64.StdEncoding.DecodeString(c["d"])
			if err != nil {
				t.Errorf("bad chunk: %v", err)
			}
			k.data.Write(d)
			k.reply(id, fmt.Sprintf(";fid=%s;st=%s;sz=%d", c["fid"], b64("PROGRESS"), k.data.Len()))
			if c["ac"] == "end_data" {
				k.reply(id, ";fid="+c["fid"]+";st="+b64("OK"))
			}
		case "finish":
			k.done = true
		}
	}
	k.seen = len(all)
	return k.done
}

func (k *kittyPeer) file(t *testing.T) []byte {
	if !k.zip {
		return k.data.Bytes()
	}
	zr, err := zlib.NewReader(bytes.NewReader(k.data.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// bigFile writes a file of many chunks into ws and returns its content.
func bigFile(t *testing.T, ws string) []byte {
	var b strings.Builder
	for i := range 3000 {
		fmt.Fprintf(&b, "line %d: the quick brown fox %x\n", i, i*7919)
	}
	if err := os.WriteFile(filepath.Join(ws, "big.txt"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return []byte(b.String())
}

// downloadWithKitty opens big.txt, runs Download from the palette and
// plays kitty until the file is saved.
func downloadWithKitty(t *testing.T, tm *Term, want []byte) {
	t.Helper()
	tm.OpenFile("big.txt")
	tm.WaitText("line 0:", uiTimeout)
	tm.Send(KeyEsc)
	tm.settle()
	tm.Palette("File: Download to This Computer")
	peer := &kittyPeer{tm: tm}
	deadline := time.Now().Add(20 * time.Second)
	for !peer.poll(t) {
		if time.Now().After(deadline) {
			t.Fatalf("no finished kitty transfer; commands seen: %v\n%s", peer.steps, tm.Screen())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := peer.file(t); !bytes.Equal(got, want) {
		t.Fatalf("kitty got %d bytes, want %d (same content: %v)", len(got), len(want), bytes.Equal(got, want))
	}
	if peer.dest != "~/Downloads/big.txt" {
		t.Errorf("dest %q", peer.dest)
	}
	tm.WaitText("Downloaded big.txt", uiTimeout)
	// The answers never reached the editor or a dialog as keys.
	tm.settle()
	for _, junk := range []string{"5113", "ac=status", "UFJPR1JFU1M"} {
		if tm.Has(junk) {
			t.Errorf("answer text %q leaked to the screen:\n%s", junk, tm.Screen())
		}
	}
	if b, _ := os.ReadFile(filepath.Join(tm.ws, "big.txt")); !bytes.Equal(b, want) {
		t.Error("big.txt changed on disk")
	}
}

func TestDownloadKitty(t *testing.T) {
	ws := NewRepo(t)
	want := bigFile(t, ws)
	tm := Start(t, StartOpts{Workspace: ws, Env: []string{"TERMOCODE_TRANSFER=kitty"}})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	tm.settle()
	downloadWithKitty(t, tm, want)
	tm.Quit()
}

// The same through a persistent session (as over SSH): the sequences
// go UI → session server → client terminal, the answers back.
func TestDownloadKittySession(t *testing.T) {
	r := newSessionRig(t)
	want := bigFile(t, r.ws)
	r.env = append(r.env, "TERMOCODE_TRANSFER=kitty")
	tm := r.start()
	downloadWithKitty(t, tm, want)
	tm.Quit()
	r.waitSocket(false)
}
