package app

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

func webTestChunk(id string, off int64, b []byte) *webproto.UploadChunkArgs {
	a, err := webproto.DecodeArgs(webproto.OpFileUploadChunk, []byte(`{"id":"`+id+`","off":`+itoa(int(off))+`,"data":"`+base64.StdEncoding.EncodeToString(b)+`"}`))
	if err != nil {
		panic(err)
	}
	return a.(*webproto.UploadChunkArgs)
}

// webTestUpload runs begin and all chunks of body to rel and returns the
// upload id and whether the name existed.
func webTestUpload(t *testing.T, r *webFilesRig, rel string, body []byte) (string, bool) {
	t.Helper()
	var st webproto.UploadStarted
	f := r.call(webproto.OpFileUploadBegin, &webproto.UploadBeginArgs{Path: rel, Size: int64(len(body))})
	if f["t"] != "res" {
		t.Fatalf("begin %s: %v", rel, f)
	}
	webTestData(t, f, &st)
	for off := 0; off < len(body); off += st.Chunk {
		end := min(off+st.Chunk, len(body))
		if f := r.call(webproto.OpFileUploadChunk, webTestChunk(st.ID, int64(off), body[off:end])); f["t"] != "res" {
			t.Fatalf("chunk at %d: %v", off, f)
		}
	}
	return st.ID, st.Exists
}

// Spec (WEB-49): an upload is staged outside the workspace (0600) and
// moves in only under a free name; chunks must come in order and stay
// within the size; replacing a file waits for a laptop card; a path
// outside the workspace is refused; the staged files go when the hub
// stops.
func TestUploadStagedAndReplaceNeedsLaptop(t *testing.T) {
	r := newWebFilesRig(t)
	r.m.output = newOutputStore()
	r.write("in/old.txt", "old\n")
	body := bytes.Repeat([]byte("0123456789abcdef"), (webproto.TransferChunk/16)+100) // more than one chunk
	id, exists := webTestUpload(t, r, "in/new.bin", body)
	if exists {
		t.Fatal("a new name said it exists")
	}
	u := r.h.ups.get(id, r.c.id)
	if u == nil || strings.HasPrefix(u.tmp, r.root) {
		t.Fatalf("staged in the workspace: %+v", u)
	}
	if fi, err := os.Stat(u.tmp); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("staged file %v %v", fi, err)
	}
	if _, err := os.Stat(filepath.Join(r.root, "in", "new.bin")); err == nil {
		t.Fatal("in the workspace before save")
	}
	if f := r.call(webproto.OpFileUploadSave, &webproto.UploadArgs{ID: id}); f["t"] != "res" {
		t.Fatalf("save: %v", f)
	}
	if b, _ := os.ReadFile(filepath.Join(r.root, "in", "new.bin")); !bytes.Equal(b, body) {
		t.Fatalf("saved %d bytes, want %d", len(b), len(body))
	}
	if _, err := os.Stat(u.tmp); err == nil {
		t.Error("the staged file stayed")
	}

	// Out of step and past the size.
	var st webproto.UploadStarted
	webTestData(t, r.call(webproto.OpFileUploadBegin, &webproto.UploadBeginArgs{Path: "in/x.txt", Size: 4}), &st)
	if f := r.call(webproto.OpFileUploadChunk, webTestChunk(st.ID, 2, []byte("ab"))); webErrCode(f) != "conflict" {
		t.Errorf("a chunk out of order: %v", f)
	}
	webTestData(t, r.call(webproto.OpFileUploadBegin, &webproto.UploadBeginArgs{Path: "in/x.txt", Size: 2}), &st)
	if f := r.call(webproto.OpFileUploadChunk, webTestChunk(st.ID, 0, []byte("abc"))); webErrCode(f) != "conflict" {
		t.Errorf("past the size: %v", f)
	}
	if f := r.call(webproto.OpFileUploadSave, &webproto.UploadArgs{ID: st.ID}); webErrCode(f) != "not_found" {
		t.Errorf("save of a dropped upload: %v", f)
	}

	// Over an existing file: save refuses; replace asks the laptop.
	id, exists = webTestUpload(t, r, "in/old.txt", []byte("new\n"))
	if !exists {
		t.Fatal("an existing name said it is free")
	}
	if f := r.call(webproto.OpFileUploadSave, &webproto.UploadArgs{ID: id}); webErrCode(f) != "conflict" {
		t.Fatalf("save over a file: %v", f)
	}
	var ref webproto.AskRef
	webTestData(t, r.call(webproto.OpFileUploadReplace, &webproto.UploadArgs{ID: id}), &ref)
	if !r.m.confirmOpen || r.m.confirmKind != confirmKindWebAsk || r.m.confirm.SafeID() != "cancel" {
		t.Fatal("no laptop card")
	}
	if v := r.m.confirm.View(); !strings.Contains(v, "Replace the file (4 B) in/old.txt") {
		t.Errorf("card:\n%s", v)
	}
	if b, _ := os.ReadFile(filepath.Join(r.root, "in", "old.txt")); string(b) != "old\n" {
		t.Fatal("replaced before the laptop said so")
	}
	r.m.confirmOpen = false
	webDrain(t, r.m, r.m.handleConfirmSelect("confirm"))
	if b, _ := os.ReadFile(filepath.Join(r.root, "in", "old.txt")); string(b) != "new\n" {
		t.Fatalf("not replaced after the laptop's yes: %q", b)
	}

	// Outside the workspace through a link.
	outside := filepath.Join(filepath.Dir(r.root), "outside-up")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(r.root, "out")); err != nil {
		t.Fatal(err)
	}
	if f := r.call(webproto.OpFileUploadBegin, &webproto.UploadBeginArgs{Path: "out/x.txt", Size: 1}); webErrCode(f) != "forbidden" {
		t.Errorf("an upload through a link out: %v", f)
	}

	webTestData(t, r.call(webproto.OpFileUploadBegin, &webproto.UploadBeginArgs{Path: "in/y.txt", Size: 1}), &st)
	dir := r.h.ups.dir
	r.h.stop()
	if _, err := os.Stat(dir); err == nil {
		t.Error("the staging folder stayed after the hub stopped")
	}
}

// Spec (WEB-49): a download comes in chunks with the file's size and
// time; a file that may hold secrets (.env, a key, credentials) is
// refused; a folder is refused; a file that changed between chunks fails.
func TestDownloadChunksAndRefusesSecrets(t *testing.T) {
	r := newWebFilesRig(t)
	r.m.output = newOutputStore()
	body := strings.Repeat("x", webproto.TransferChunk+10)
	r.write("dist/app.bin", body)
	var c webproto.FileChunk
	webTestData(t, r.call(webproto.OpFileDownload, &webproto.DownloadArgs{Path: "dist/app.bin"}), &c)
	if c.Size != int64(len(body)) || c.Next != int64(webproto.TransferChunk) || c.Mtime == 0 {
		t.Fatalf("first chunk %+v", c)
	}
	got, _ := base64.StdEncoding.DecodeString(c.Data)
	from := c.Next
	c = webproto.FileChunk{}
	webTestData(t, r.call(webproto.OpFileDownload, &webproto.DownloadArgs{Path: "dist/app.bin", From: from}), &c)
	more, _ := base64.StdEncoding.DecodeString(c.Data)
	if string(got)+string(more) != body || c.Next != 0 {
		t.Fatalf("chunks %d + %d bytes, next %d", len(got), len(more), c.Next)
	}
	for _, p := range []string{".env", "conf/.env.local", "keys/id_ed25519", "server.pem", ".git-credentials"} {
		r.write(p, "SECRET=1\n")
		f := r.call(webproto.OpFileDownload, &webproto.DownloadArgs{Path: p})
		if webErrCode(f) != "forbidden" {
			t.Errorf("%s: %v", p, f)
		}
		if b, _ := json.Marshal(f); strings.Contains(string(b), "SECRET") {
			t.Errorf("%s leaked", p)
		}
	}
	if f := r.call(webproto.OpFileDownload, &webproto.DownloadArgs{Path: "dist"}); webErrCode(f) != "bad_request" {
		t.Errorf("a folder: %v", f)
	}
	if a := webTestAudit(r.m); !strings.Contains(a, "file.download  dist/app.bin  by=web:") {
		t.Errorf("audit:\n%s", a)
	}
}
