package search

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeRg puts a shell script named rg first on PATH. The script body runs
// with the search root as its working folder.
func fakeRg(t *testing.T, body string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script fakes need a POSIX shell")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "rg"), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if !Available() {
		t.Fatal("fake rg not found on PATH")
	}
}

// noRg hides any real ripgrep so the built-in walker runs.
func noRg(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	if Available() {
		t.Fatal("rg still on PATH")
	}
}

const rgMatchA = `{"type":"begin","data":{"path":{"text":"a.go"}}}
{"type":"match","data":{"path":{"text":"a.go"},"lines":{"text":"x foo y\n"},"line_number":3,"submatches":[{"match":{"text":"foo"},"start":2,"end":5}]}}
{"type":"end","data":{"path":{"text":"a.go"}}}`

func TestSearchUsesRipgrepAndMakesPathsAbsolute(t *testing.T) {
	fakeRg(t, "cat <<'EOF'\n"+rgMatchA+"\nEOF")
	dir := t.TempDir()
	rs, sum, err := Collect(context.Background(), []string{dir}, "foo", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 || sum.Files != 1 {
		t.Fatalf("got %+v %+v", rs, sum)
	}
	if want := filepath.Join(dir, "a.go"); rs[0].Path != want {
		t.Fatalf("path %q, want %q", rs[0].Path, want)
	}
	if rs[0].Line != 3 || rs[0].Col != 3 || rs[0].Preview != "x foo y" {
		t.Fatalf("result %+v", rs[0])
	}
	// Run keeps paths relative to the root.
	rel, err := Run(dir, "foo")
	if err != nil || len(rel) != 1 || rel[0].Path != "a.go" {
		t.Fatalf("Run = %+v, %v", rel, err)
	}
	abs, err := RunDirs([]string{dir}, "foo")
	if err != nil || len(abs) != 1 || !filepath.IsAbs(abs[0].Path) {
		t.Fatalf("RunDirs = %+v, %v", abs, err)
	}
}

func TestRipgrepExitOneMeansNoMatches(t *testing.T) {
	fakeRg(t, "exit 1")
	rs, sum, err := Collect(context.Background(), []string{t.TempDir()}, "foo", Options{})
	if err != nil || len(rs) != 0 || sum.Total != 0 {
		t.Fatalf("rs=%v sum=%+v err=%v", rs, sum, err)
	}
	if got, err := Run(t.TempDir(), "foo"); got != nil || err != nil {
		t.Fatalf("Run = %v, %v", got, err)
	}
}

func TestRipgrepErrorShowsFirstStderrLine(t *testing.T) {
	fakeRg(t, "echo 'regex parse error:' >&2\necho '    (foo' >&2\nexit 2")
	_, _, err := Collect(context.Background(), []string{t.TempDir()}, "(foo", Options{Regex: true})
	if err == nil || err.Error() != "regex parse error:" {
		t.Fatalf("err = %v", err)
	}
}

func TestRipgrepErrorWithoutStderr(t *testing.T) {
	fakeRg(t, "exit 3")
	_, _, err := Collect(context.Background(), []string{t.TempDir()}, "foo", Options{})
	if err == nil || !strings.Contains(err.Error(), "exit status 3") {
		t.Fatalf("err = %v", err)
	}
}

func TestOneBadRootDoesNotHideMatchesInAnother(t *testing.T) {
	// The root named "bad" fails; the other one matches.
	fakeRg(t, `case "$(pwd)" in */bad) echo 'bad: Permission denied' >&2; exit 2;; esac
cat <<'EOF'
`+rgMatchA+`
EOF`)
	bad := filepath.Join(t.TempDir(), "bad")
	if err := os.Mkdir(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	good := t.TempDir()
	rs, _, err := Collect(context.Background(), []string{"", bad, good}, "foo", Options{})
	if err != nil || len(rs) != 1 {
		t.Fatalf("rs=%+v err=%v", rs, err)
	}
	// With only the bad root, its error comes back.
	_, _, err = Collect(context.Background(), []string{bad}, "foo", Options{})
	if err == nil || !strings.Contains(err.Error(), "Permission denied") {
		t.Fatalf("err = %v", err)
	}
}

func TestHungRipgrepStopsOnCancel(t *testing.T) {
	mark := filepath.Join(t.TempDir(), "started")
	fakeRg(t, "touch '"+mark+"'\nexec sleep 30")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, _, err := Collect(ctx, []string{t.TempDir()}, "foo", Options{})
		done <- err
	}()
	// Cancel only once rg is really running.
	deadline := time.Now().Add(20 * time.Second)
	for {
		if _, err := os.Stat(mark); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fake rg never started")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("search did not stop after cancel")
	}
}

func TestBrokenRipgrepBinaryIsAnError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX exec only")
	}
	bin := t.TempDir()
	// Executable, but not a program the system can start.
	if err := os.WriteFile(filepath.Join(bin, "rg"), []byte{0x7f, 'E', 'L', 'F', 0, 0, 0}, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	_, _, err := Collect(context.Background(), []string{t.TempDir()}, "foo", Options{})
	if err == nil {
		t.Fatal("want an error from a broken rg")
	}
}

func TestRipgrepStreamSkipsJunkAndKeepsContext(t *testing.T) {
	fakeRg(t, `cat <<'EOF'
not json at all
{broken
{"type":"summary","data":{}}
{"type":"begin","data":{"path":{"text":"a.go"}}}
{"type":"match","data":{"path":{"text":"a.go"},"lines":{"text":"foo 1\n"},"line_number":1,"submatches":[{"match":{"text":"foo"},"start":0,"end":3}]}}
{"type":"context","data":{"path":{"text":"a.go"},"lines":{"text":"mid\n"},"line_number":2}}
{"type":"match","data":{"path":{"text":"a.go"},"lines":{"text":"foo 3\n"},"line_number":3,"submatches":[{"match":{"text":"foo"},"start":0,"end":3}]}}
{"type":"end","data":{"path":{"text":"a.go"}}}
EOF`)
	rs, _, err := Collect(context.Background(), []string{t.TempDir()}, "foo", Options{Context: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 {
		t.Fatalf("got %+v", rs)
	}
	// The line between two matches is After of the first and Before of the second.
	if len(rs[0].After) != 1 || rs[0].After[0].Text != "mid" || len(rs[1].Before) != 1 || rs[1].Before[0].Line != 2 {
		t.Fatalf("context: %+v", rs)
	}
}

func TestMaxResultsStopsDeliveringLaterFiles(t *testing.T) {
	noRg(t)
	dir := t.TempDir()
	for _, n := range []string{"a.txt", "b.txt", "c.txt"} {
		writeFile(t, dir, n, strings.Repeat("hit\n", 10))
	}
	var calls int
	sum, err := Search(context.Background(), []string{dir}, "hit", Options{MaxResults: 10}, func([]Result) { calls++ })
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || sum.Shown != 10 || sum.Total != 30 || sum.Files != 1 {
		t.Fatalf("calls=%d sum=%+v", calls, sum)
	}
}

func TestFallbackRegexThatMatchesEmptyShowsNothingEmpty(t *testing.T) {
	noRg(t)
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "abc\nxx\n")
	rs, _, err := Collect(context.Background(), []string{dir}, "x*", Options{Regex: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 || rs[0].Line != 2 || len(rs[0].Matches) != 1 || rs[0].Matches[0] != (MatchRange{0, 2}) {
		t.Fatalf("got %+v", rs)
	}
}

func TestSearchStopsBeforeNextRootWhenCancelled(t *testing.T) {
	noRg(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := Collect(ctx, []string{t.TempDir()}, "foo", Options{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestCountLiteralWithRipgrep(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "x.y x.y\n")
	writeFile(t, dir, "bin.dat", "x.y\x00")
	// rg lists both files (NUL separated); the binary one is skipped and a
	// listed file that is gone is ignored.
	fakeRg(t, `printf 'a.txt\000bin.dat\000gone.txt\000'`)
	got, err := CountLiteral([]string{"", dir}, "x.y")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Count != 2 || got[0].Path != filepath.Join(dir, "a.txt") {
		t.Fatalf("got %+v", got)
	}
}

func TestCountLiteralRipgrepErrors(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "needle\n")

	fakeRg(t, "exit 1")
	if got, err := CountLiteral([]string{dir}, "needle"); err != nil || len(got) != 0 {
		t.Fatalf("exit 1: %v %v", got, err)
	}

	fakeRg(t, "echo 'boom happened' >&2\necho second >&2\nexit 2")
	if _, err := CountLiteral([]string{dir}, "needle"); err == nil || err.Error() != "boom happened" {
		t.Fatalf("stderr error: %v", err)
	}

	fakeRg(t, "exit 2")
	if _, err := CountLiteral([]string{dir}, "needle"); err == nil || !strings.Contains(err.Error(), "exit status 2") {
		t.Fatalf("bare error: %v", err)
	}

	// Some files unreadable: rg exits 2 but still lists the good files,
	// which are counted.
	fakeRg(t, "echo 'secret: Permission denied' >&2\nprintf 'a.txt\\000'\nexit 2")
	got, err := CountLiteral([]string{dir}, "needle")
	if err != nil || len(got) != 1 || got[0].Count != 1 {
		t.Fatalf("partial: %+v %v", got, err)
	}
}

func TestCountLiteralWalkerSkipsJunk(t *testing.T) {
	noRg(t)
	dir := t.TempDir()
	writeFile(t, dir, "src/a.txt", "needle needle\n")
	writeFile(t, dir, ".git/config", "needle\n")
	writeFile(t, dir, "node_modules/x/y.js", "needle\n")
	writeFile(t, dir, "img.bin", "needle\x00")
	writeFile(t, dir, "big.log", "needle\n"+strings.Repeat("z", maxFallbackFileSize))
	if got, err := CountLiteral([]string{dir}, ""); got != nil || err != nil {
		t.Fatalf("empty needle: %v %v", got, err)
	}
	got, err := CountLiteral([]string{dir}, "needle")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Count != 2 || got[0].Path != filepath.Join(dir, "src", "a.txt") {
		t.Fatalf("got %+v", got)
	}
}

func TestReplaceLiteralInFileEdgeCases(t *testing.T) {
	dir := t.TempDir()
	if n, err := ReplaceLiteralInFile(filepath.Join(dir, "missing"), "a", "b"); n != 0 || err == nil {
		t.Fatalf("missing file: n=%d err=%v", n, err)
	}
	writeFile(t, dir, "a.txt", "abc\n")
	p := filepath.Join(dir, "a.txt")
	if n, err := ReplaceLiteralInFile(p, "", "b"); n != 0 || err != nil {
		t.Fatalf("empty find: n=%d err=%v", n, err)
	}
	if n, err := ReplaceLiteralInFile(p, "zzz", "b"); n != 0 || err != nil {
		t.Fatalf("no hit: n=%d err=%v", n, err)
	}
	if err := os.Chmod(p, 0o444); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() != 0 {
		if n, err := ReplaceLiteralInFile(p, "abc", "x"); n != 0 || err == nil {
			t.Fatalf("read-only file: n=%d err=%v", n, err)
		}
		if b, _ := os.ReadFile(p); string(b) != "abc\n" {
			t.Fatalf("read-only file changed: %q", b)
		}
	}
	if err := os.Chmod(p, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(p, 0o644) })
	if os.Geteuid() != 0 {
		if n, err := ReplaceLiteralInFile(p, "abc", "x"); n != 0 || err == nil {
			t.Fatalf("unreadable file: n=%d err=%v", n, err)
		}
	}
}

func TestFallbackSkipsBinaryBigHiddenAndUnreadable(t *testing.T) {
	noRg(t)
	dir := t.TempDir()
	writeFile(t, dir, "ok.txt", "hit\n")
	// The NUL sits after the first 8 KiB, so the file counts as text.
	writeFile(t, dir, "late-nul.txt", "hit\n"+strings.Repeat("a", 9000)+"\x00")
	writeFile(t, dir, "bin.dat", "hit\x00")
	writeFile(t, dir, "big.log", "hit\n"+strings.Repeat("z", maxFallbackFileSize))
	writeFile(t, dir, ".hidden/x.txt", "hit\n")
	writeFile(t, dir, "locked/x.txt", "hit\n")
	locked := filepath.Join(dir, "locked")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	rs, _, err := Collect(context.Background(), []string{dir}, "hit", Options{})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range rs {
		names = append(names, filepath.Base(r.Path))
	}
	want := map[string]bool{"ok.txt": true, "late-nul.txt": true}
	if os.Geteuid() == 0 {
		want["x.txt"] = true // root reads the locked folder
	}
	if len(names) != len(want) {
		t.Fatalf("files %v, want %v", names, want)
	}
	for _, n := range names {
		if !want[n] {
			t.Fatalf("unexpected file %s in %v", n, names)
		}
	}
}

func TestFallbackBadRegexIsAnError(t *testing.T) {
	noRg(t)
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "x\n")
	_, _, err := Collect(context.Background(), []string{dir}, "(unclosed", Options{Regex: true})
	if err == nil || !strings.Contains(err.Error(), "missing closing )") {
		t.Fatalf("err = %v", err)
	}
	// As a literal the same text is fine.
	if _, _, err := Collect(context.Background(), []string{dir}, "(unclosed", Options{}); err != nil {
		t.Fatal(err)
	}
}

func TestFallbackCancelStopsWalk(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "hit\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := runFallback(ctx, dir, "hit", Options{}, func([]Result) { t.Error("result after cancel") })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if err := runFallback(context.Background(), dir, "", Options{}, func([]Result) { t.Error("empty query matched") }); err != nil {
		t.Fatal(err)
	}
}

func TestFallbackHugeFileCapsPerFileAndLongLines(t *testing.T) {
	noRg(t)
	dir := t.TempDir()
	writeFile(t, dir, "many.txt", strings.Repeat("hit\n", maxPerFile+50))
	long := strings.Repeat("a", MaxColumns-3) + "hit" + strings.Repeat("b", 10) + "hit\n"
	writeFile(t, dir, "long.txt", "ctx "+strings.Repeat("c", MaxColumns+20)+"\n"+long)
	rs, sum, err := Collect(context.Background(), []string{dir}, "hit", Options{Context: 1})
	if err != nil {
		t.Fatal(err)
	}
	per := map[string][]Result{}
	for _, r := range rs {
		per[filepath.Base(r.Path)] = append(per[filepath.Base(r.Path)], r)
	}
	if n := len(per["many.txt"]); n != maxPerFile {
		t.Fatalf("many.txt gave %d results, want %d", n, maxPerFile)
	}
	if sum.Total != maxPerFile+1 {
		t.Fatalf("total %d", sum.Total)
	}
	l := per["long.txt"]
	if len(l) != 1 {
		t.Fatalf("long.txt: %+v", l)
	}
	r := l[0]
	if len(r.Preview) != MaxColumns {
		t.Fatalf("preview %d bytes, want %d", len(r.Preview), MaxColumns)
	}
	// The first hit ends exactly at the cut and stays; the second is cut off.
	if len(r.Matches) != 1 || r.Matches[0].End != MaxColumns {
		t.Fatalf("matches %+v", r.Matches)
	}
	if len(r.Before) != 1 || len(r.Before[0].Text) != MaxColumns {
		t.Fatalf("long context line not clipped: %+v", r.Before)
	}
}

func TestGlobMatchForms(t *testing.T) {
	cases := []struct {
		pat, rel string
		want     bool
	}{
		{"", "a.go", false},
		{"./*.go", "a.go", true},
		{"**/*.go", "x/y/a.go", true},
		{"vendor/", "vendor/lib/a.go", true},
		{"vendor/**", "vendor/lib/a.go", true},
		{"vendor", "src/vendor/a.go", true},
		{"vendor", "src/a.go", false},
		{"src/app", "src/app/x/y.go", true},
		{"src/*.go", "src/a.go", true},
		{"src/app", "src/other/y.go", false},
	}
	for _, c := range cases {
		if got := globMatch(c.pat, c.rel, filepath.Base(c.rel)); got != c.want {
			t.Errorf("globMatch(%q, %q) = %v, want %v", c.pat, c.rel, got, c.want)
		}
	}
}

func TestFallbackExcludeAndIncludeFiles(t *testing.T) {
	noRg(t)
	dir := t.TempDir()
	writeFile(t, dir, "src/app/a.go", "hit\n")
	writeFile(t, dir, "src/app/a_test.go", "hit\n")
	writeFile(t, dir, "src/lib/b.go", "hit\n")
	writeFile(t, dir, "docs/c.md", "hit\n")
	rs, _, err := Collect(context.Background(), []string{dir}, "hit",
		Options{Include: []string{"src/**"}, Exclude: []string{"!*_test.go", "src/lib"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 || filepath.Base(rs[0].Path) != "a.go" {
		t.Fatalf("got %+v", rs)
	}
}

func TestAbsJoinAndFirstLine(t *testing.T) {
	if absJoin("/x", "") != "" {
		t.Fatal("empty rel")
	}
	abs := filepath.Join(t.TempDir(), "f")
	if absJoin("/x", abs) != abs {
		t.Fatal("absolute rel must stay")
	}
	if firstLine("one") != "one" || firstLine("one\ntwo") != "one" {
		t.Fatal("firstLine")
	}
}
