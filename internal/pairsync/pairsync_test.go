package pairsync

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/workspace"
	"github.com/amin-jalali/termocode/internal/workspace/localhost"
)

func e(size, sec int64) Entry { return Entry{Size: size, MTime: sec * int64(time.Second), Mode: 0o644} }

func ops(p Plan) map[string]Op {
	out := map[string]Op{}
	for _, a := range p.Actions {
		out[a.Path] = a.Op
	}
	return out
}

func TestDiffEveryCase(t *testing.T) {
	base := Base{
		Local:  Snapshot{"same": e(1, 1), "lchg": e(1, 1), "rchg": e(1, 1), "both": e(1, 1), "ldel": e(1, 1), "rdel": e(1, 1), "gone": e(1, 1), "lchg-rdel": e(1, 1), "rchg-ldel": e(1, 1)},
		Remote: Snapshot{"same": e(1, 5), "lchg": e(1, 5), "rchg": e(1, 5), "both": e(1, 5), "ldel": e(1, 5), "rdel": e(1, 5), "gone": e(1, 5), "lchg-rdel": e(1, 5), "rchg-ldel": e(1, 5)},
	}
	local := Snapshot{"same": e(1, 1), "lchg": e(2, 2), "rchg": e(1, 1), "both": e(2, 2), "rdel": e(1, 1), "lchg-rdel": e(3, 3), "lnew": e(1, 1), "bnew": e(1, 1)}
	remote := Snapshot{"same": e(1, 5), "lchg": e(1, 5), "rchg": e(4, 6), "both": e(3, 6), "ldel": e(1, 5), "rchg-ldel": e(9, 9), "rnew": e(1, 1), "bnew": e(1, 1)}
	got := ops(Diff(base, local, remote))
	want := map[string]Op{
		"lchg": OpPush, "lnew": OpPush,
		"rchg": OpPull, "rnew": OpPull,
		"ldel": OpDeleteRemote, "rdel": OpDeleteLocal,
		"both": OpConflict, "bnew": OpConflict, "lchg-rdel": OpConflict, "rchg-ldel": OpConflict,
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for p, op := range want {
		if got[p] != op {
			t.Errorf("%s: got %v, want %v", p, got[p], op)
		}
	}
	if _, ok := got["same"]; ok {
		t.Error("an unchanged file must not be in the plan")
	}
	if _, ok := got["gone"]; ok {
		t.Error("a file deleted on both sides must not be in the plan")
	}
}

func TestDiffIsSortedAndCounts(t *testing.T) {
	p := Diff(Base{}, Snapshot{"b": e(1, 1), "a": e(1, 1)}, Snapshot{"c": e(1, 1)})
	if len(p.Actions) != 3 || p.Actions[0].Path != "a" || p.Actions[1].Path != "b" || p.Actions[2].Path != "c" {
		t.Fatalf("not sorted: %+v", p.Actions)
	}
	if p.Count(OpPush) != 2 || p.Count(OpPull) != 1 || p.Count(OpConflict) != 0 {
		t.Fatalf("counts: %+v", p.Actions)
	}
	if (Plan{}).Empty() != true || p.Empty() {
		t.Fatal("Empty is wrong")
	}
}

func TestIgnore(t *testing.T) {
	ig := NewIgnore("# comment\n*.log\nbuild/\n/dist\ndocs/tmp\n!keep.log\n\n")
	for _, p := range []string{".git/config", "a/node_modules/x.js", "x.log", "a/b/c.log", "build/out", "src/build/out", "dist/a", "docs/tmp/a", ".termocode/x"} {
		if !ig.Match(p) {
			t.Errorf("%s should be ignored", p)
		}
	}
	for _, p := range []string{"main.go", "src/dist/a", "logs/a.txt", "docs/tmpx", "keep.go"} {
		if ig.Match(p) {
			t.Errorf("%s should be synced", p)
		}
	}
	if (*Ignore)(nil).Match("a") {
		t.Error("a nil Ignore matches nothing")
	}
}

func TestParseStatList(t *testing.T) {
	out := "12 1700000000 644 ./a.txt\n0 1700000001 755 ./bin/run me\nbad line\n5 1 644 ./x.log\n3 2 600 ./.git/HEAD\n"
	snap, err := ParseStatList(out, NewIgnore("*.log"), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap) != 2 {
		t.Fatalf("got %v", snap)
	}
	if got := snap["a.txt"]; got.Size != 12 || got.MTime != 1700000000*int64(time.Second) || got.Mode != 0o644 {
		t.Errorf("a.txt: %+v", got)
	}
	if got := snap["bin/run me"]; got.Mode != 0o755 {
		t.Errorf("a name with a space: %+v", got)
	}
	if _, err := ParseStatList(out, nil, 1); !errors.Is(err, ErrTooMany) {
		t.Fatalf("want ErrTooMany, got %v", err)
	}
}

type fakeExec struct {
	out  string
	code int
	argv []string
}

func (f *fakeExec) Capture(_ context.Context, _ string, argv ...string) (string, string, int, error) {
	f.argv = argv
	return f.out, "", f.code, nil
}
func (f *fakeExec) CaptureInput(ctx context.Context, dir string, _ []byte, argv ...string) (string, string, int, error) {
	return f.Capture(ctx, dir, argv...)
}
func (f *fakeExec) Stream(context.Context, string, ...string) (io.ReadCloser, func(), error) {
	return nil, nil, errors.New("no stream")
}

func TestExecListRunsOneFindAndPrunesDefaults(t *testing.T) {
	x := &fakeExec{out: "1 2 644 ./a\n"}
	snap, err := ExecList(context.Background(), x, "/app", nil, 10)
	if err != nil || len(snap) != 1 {
		t.Fatalf("snap %v err %v", snap, err)
	}
	line := strings.Join(x.argv, " ")
	if x.argv[0] != "sh" || !strings.Contains(line, "-prune") || !strings.Contains(line, "node_modules") || !strings.Contains(line, "/app") {
		t.Fatalf("argv: %q", x.argv)
	}
	x = &fakeExec{code: 127}
	if _, err := ExecList(context.Background(), x, "/app", nil, 10); err == nil {
		t.Fatal("a failed find with no output is an error")
	}
}

func tempWS(t *testing.T, files map[string]string) (workspace.FS, string) {
	t.Helper()
	dir := t.TempDir()
	for p, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ws, err := localhost.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	return ws.Host.FS(), dir
}

func TestWalkSkipsIgnoredAndCaps(t *testing.T) {
	fsys, _ := tempWS(t, map[string]string{"a.go": "a", "sub/b.go": "bb", ".git/HEAD": "x", "node_modules/m.js": "m"})
	snap, err := Walk(context.Background(), fsys, NewIgnore(""), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap) != 2 || snap["sub/b.go"].Size != 2 {
		t.Fatalf("got %v", snap)
	}
	if _, err := Walk(context.Background(), fsys, NewIgnore(""), 1); !errors.Is(err, ErrTooMany) {
		t.Fatalf("want ErrTooMany, got %v", err)
	}
}

func read(t *testing.T, dir, p string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
	if err != nil {
		return "<missing>"
	}
	return string(b)
}

// TestTwoWaySyncRoundTrip runs two rounds between two local folders:
// the first copies new files both ways and adopts equal ones; the second
// carries an edit and a delete from each side.
func TestTwoWaySyncRoundTrip(t *testing.T) {
	ctx := context.Background()
	lfs, ldir := tempWS(t, map[string]string{"only-local.txt": "L", "same.txt": "S", "differ.txt": "one"})
	rfs, rdir := tempWS(t, map[string]string{"only-remote.txt": "R", "same.txt": "S", "differ.txt": "two"})
	ig := NewIgnore("")
	scan := func() (Snapshot, Snapshot) {
		l, err := Walk(ctx, lfs, ig, 100)
		if err != nil {
			t.Fatal(err)
		}
		r, err := Walk(ctx, rfs, ig, 100)
		if err != nil {
			t.Fatal(err)
		}
		return l, r
	}

	var base Base
	l, r := scan()
	plan := ResolveSame(ctx, Diff(base, l, r), lfs, rfs, 1<<20)
	if got := ops(plan); got["same.txt"] != OpAdopt || got["differ.txt"] != OpConflict || got["only-local.txt"] != OpPush || got["only-remote.txt"] != OpPull {
		t.Fatalf("first plan: %v", got)
	}
	res := Apply(ctx, plan, lfs, rfs, l, r, 1<<20)
	if len(res.Failed) != 0 || res.Pushed != 1 || res.Pulled != 1 || res.Adopted != 1 || res.Conflicts != 1 {
		t.Fatalf("first result: %+v", res)
	}
	if read(t, rdir, "only-local.txt") != "L" || read(t, ldir, "only-remote.txt") != "R" {
		t.Fatal("new files were not copied both ways")
	}
	if read(t, ldir, "differ.txt") != "one" || read(t, rdir, "differ.txt") != "two" {
		t.Fatal("a conflict must leave both sides alone")
	}
	l2, r2 := scan()
	base = NextBase(base, plan, res.OK, l, r, l2, r2)
	if len(base.Local) != 3 || len(base.Remote) != 3 {
		t.Fatalf("base after round 1: %v / %v", base.Local, base.Remote)
	}

	// Round 2: edit and delete on both sides.
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(ldir, "same.txt"), []byte("S2-longer"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(rdir, "only-local.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(ldir, "only-remote.txt")); err != nil {
		t.Fatal(err)
	}
	l, r = scan()
	if err := GuardMassDelete(base, l, r); err != nil {
		t.Fatal(err)
	}
	plan = ResolveSame(ctx, Diff(base, l, r), lfs, rfs, 1<<20)
	if got := ops(plan); got["same.txt"] != OpPush || got["only-local.txt"] != OpDeleteLocal || got["only-remote.txt"] != OpDeleteRemote || got["differ.txt"] != OpConflict {
		t.Fatalf("second plan: %v", got)
	}
	res = Apply(ctx, plan, lfs, rfs, l, r, 1<<20)
	if len(res.Failed) != 0 || res.DeletedLocal != 1 || res.DeletedRemote != 1 || res.Pushed != 1 {
		t.Fatalf("second result: %+v", res)
	}
	if read(t, rdir, "same.txt") != "S2-longer" || read(t, ldir, "only-local.txt") != "<missing>" || read(t, rdir, "only-remote.txt") != "<missing>" {
		t.Fatal("round 2 was not applied")
	}
	l2, r2 = scan()
	base = NextBase(base, plan, res.OK, l, r, l2, r2)
	if p := Diff(base, l2, r2); p.Count(OpConflict) != 1 || len(p.Actions) != 1 {
		t.Fatalf("after round 2 only the conflict is left: %+v", p.Actions)
	}
}

func TestApplyRefusesTooLarge(t *testing.T) {
	ctx := context.Background()
	lfs, _ := tempWS(t, map[string]string{"big.bin": "0123456789"})
	rfs, rdir := tempWS(t, nil)
	l, _ := Walk(ctx, lfs, nil, 10)
	plan := Diff(Base{}, l, Snapshot{})
	res := Apply(ctx, plan, lfs, rfs, l, Snapshot{}, 4)
	if len(res.Failed) != 1 || res.OK["big.bin"] || read(t, rdir, "big.bin") != "<missing>" {
		t.Fatalf("a file over the cap must not be copied: %+v", res)
	}
}

func TestNextBaseKeepsAFileThatChangedDuringTheSync(t *testing.T) {
	pre := Snapshot{"a": e(1, 1)}
	post := Snapshot{"a": e(2, 2)} // edited while the sync ran
	old := Base{Local: Snapshot{"a": e(1, 1)}, Remote: Snapshot{"a": e(1, 1)}}
	nb := NextBase(old, Plan{}, nil, pre, Snapshot{"a": e(1, 1)}, post, Snapshot{"a": e(1, 1)})
	if nb.Local["a"] != e(1, 1) {
		t.Fatalf("the old base must stay so the edit syncs next time: %+v", nb.Local)
	}
}

func TestGuardMassDelete(t *testing.T) {
	base := Base{Local: Snapshot{"a": e(1, 1)}, Remote: Snapshot{"a": e(1, 1)}}
	if err := GuardMassDelete(base, Snapshot{}, Snapshot{"a": e(1, 1)}); err == nil {
		t.Fatal("an empty workspace with a known base must be refused")
	}
	if err := GuardMassDelete(base, Snapshot{"a": e(1, 1)}, Snapshot{}); err == nil {
		t.Fatal("an empty container folder with a known base must be refused")
	}
	if err := GuardMassDelete(Base{}, Snapshot{}, Snapshot{"a": e(1, 1)}); err != nil {
		t.Fatalf("a first sync into an empty folder is fine: %v", err)
	}
}

func TestBaseSaveLoad(t *testing.T) {
	file := filepath.Join(t.TempDir(), "pairsync", "k.json")
	b, err := LoadBase(file)
	if err != nil || len(b.Local) != 0 {
		t.Fatalf("a missing base is empty: %v %v", b, err)
	}
	b = Base{Local: Snapshot{"a": e(1, 1)}, Remote: Snapshot{"a": e(1, 2)}}
	if err := SaveBase(file, b); err != nil {
		t.Fatal(err)
	}
	got, err := LoadBase(file)
	if err != nil || got.Local["a"] != e(1, 1) || got.Remote["a"] != e(1, 2) {
		t.Fatalf("got %+v %v", got, err)
	}
	if fi, err := os.Stat(file); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("base file mode: %v %v", fi, err)
	}
	if k1, k2 := BaseKey("/w", "", "api", "/app"), BaseKey("/w", "", "api", "/srv"); k1 == k2 || len(k1) != 16 {
		t.Fatalf("keys: %q %q", k1, k2)
	}
}
