package git

import (
	"fmt"
	"strings"
	"testing"
)

// FuzzGitOutputParsers feeds any text to the parsers of git's porcelain
// output (status, branch status, blame, numstat, decorations, clone
// progress, log records). None may panic, and what they return must come
// from the input.
func FuzzGitOutputParsers(f *testing.F) {
	for _, s := range []string{
		" M a.go\x00R  new.go\x00old.go\x00?? x y\x00",
		"# branch.oid abc123\x00# branch.head main\x00# branch.ab +2 -1\x001 .M N... 100644 100644 100644 a b f.go\x002 R. N... 100644 100644 100644 a b R100 new\x00old\x00u UU N... 1 2 3 4 h1 h2 h3 c.go\x00? n.txt\x00",
		"# branch.oid (initial)\x00# branch.head (detached)\x00",
		"abcdef1234567 1 1 2\nauthor Me\nauthor-time 1700000000\nsummary fix it\n\tline one\nabcdef1234567 2 2\n\tline two\n0000000000000 3 3 1\n\tnew\n",
		":100644 100644 abc def M\x00a.go\x00:100644 100644 abc def R100\x00old\x00new\x003\t1\ta.go\x00-\t-\tbin\x00",
		"HEAD -> refs/heads/main, tag: refs/tags/v1, refs/remotes/origin/main, refs/remotes/origin/HEAD",
		"remote: Counting objects:  45% (450/1000)",
		"Cloning into 'x'...",
		"abc\x1fabcdef\x1fp1 p2\x1fsubj\x1fme\x1f1700000000\x1fHEAD -> refs/heads/main\x1e\nabd\x1fabdef0\x1f\x1froot\x1fme\x1fx\x1f\x1e",
		" 4 files changed, 88 insertions(+), 24 deletions(-)",
		"",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		for _, fs := range parseStatusZ(s) {
			if len(fs.Code) != 2 || !strings.Contains(s, fs.Path) {
				t.Fatalf("status entry %+v not from input", fs)
			}
		}
		_ = parseStatusV2(s)
		_ = parseBlamePorcelain(s)
		for _, l := range parseBlameRange(s) {
			if l.Hash != "uncommitted" && !strings.Contains(s, l.Hash) {
				t.Fatalf("blame hash %q not from input", l.Hash)
			}
		}
		_ = parseRawNumstat(s)
		_, _ = parseDecorations(s)
		if p, ok := ParseCloneProgress(s); ok && (p.Percent < -1 || p.Percent > 100) {
			t.Fatalf("clone progress %d%% out of range", p.Percent)
		}
		_ = parseCommitLog(s)
		_, _ = SplitUserToken(s)
		files, add, del := parseShortstat(s)
		if files < 0 || add < 0 || del < 0 {
			t.Fatalf("negative shortstat %d %d %d from %q", files, add, del, s)
		}
	})
}

// FuzzShortstatRoundTrip: git's summary line reads back as its numbers.
func FuzzShortstatRoundTrip(f *testing.F) {
	f.Add(uint32(4), uint32(88), uint32(24))
	f.Add(uint32(1), uint32(0), uint32(1))
	f.Fuzz(func(t *testing.T, a, b, c uint32) {
		s := fmt.Sprintf(" %d files changed, %d insertions(+), %d deletions(-)", a, b, c)
		files, add, del := parseShortstat(s)
		if files != int(a) || add != int(b) || del != int(c) {
			t.Fatalf("%q -> %d %d %d", s, files, add, del)
		}
	})
}

// FuzzConflicts reads any text for conflict blocks. Blocks are in order,
// do not overlap, point at real marker lines, and resolving one replaces
// exactly its lines.
func FuzzConflicts(f *testing.F) {
	for _, s := range []string{
		"a\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> feature\nb\n",
		"<<<<<<< ours\r\nx\r\n||||||| base\r\ny\r\n=======\r\nz\r\n>>>>>>> theirs\r\n",
		"<<<<<<< a\n<<<<<<< b\nx\n=======\ny\n>>>>>>> c\n=======\n>>>>>>>\n",
		"<<<<<<<\n=======\n>>>>>>>",
		"<<<<<<< no end\n=======\n",
		"=======\n>>>>>>>\n",
	} {
		f.Add(s, uint8(0))
	}
	f.Fuzz(func(t *testing.T, text string, pick uint8) {
		lines := SplitLines(text)
		blocks := ParseConflicts(text)
		if len(blocks) > 0 && !HasConflicts(text) {
			t.Fatal("blocks found but HasConflicts is false")
		}
		prevEnd := -1
		for _, b := range blocks {
			if b.Start <= prevEnd || b.Start >= b.Sep || b.Sep >= b.End || b.End >= len(lines) {
				t.Fatalf("bad block %+v after %d (%d lines)", b, prevEnd, len(lines))
			}
			if b.Base >= 0 && (b.Base <= b.Start || b.Base >= b.Sep) {
				t.Fatalf("bad base in %+v", b)
			}
			if !isConflictMarker(lines[b.Start], conflictOursMarker) || !isConflictMarker(lines[b.Sep], conflictSepMarker) || !isConflictMarker(lines[b.End], conflictTheirsMarker) {
				t.Fatalf("block %+v is not on marker lines", b)
			}
			if c, ok := ConflictAt(blocks, b.Sep); !ok || c.Start != b.Start {
				t.Fatalf("ConflictAt(%d) misses %+v", b.Sep, b)
			}
			prevEnd = b.End
		}
		if len(blocks) == 0 {
			return
		}
		b := blocks[int(pick)%len(blocks)]
		r := Resolution(int(pick) % 3)
		out := ResolveConflict(text, b, r)
		eol := "\n"
		if strings.Contains(text, "\r\n") {
			eol = "\r\n"
		}
		if !strings.HasPrefix(out, strings.Join(lines[:b.Start], eol)) || !strings.Contains(out, strings.Join(b.Replacement(r), eol)) {
			t.Fatalf("resolve lost text:\n%q\n%q", text, out)
		}
		if got := ParseConflicts(out); len(got) > len(blocks) {
			t.Fatalf("resolving made more blocks: %d -> %d\n%q\n%q", len(blocks), len(got), text, out)
		}
	})
}

// FuzzLinePatch builds line patches from any diff. A patch git gets has
// hunk headers whose counts match their bodies.
func FuzzLinePatch(f *testing.F) {
	for _, s := range []string{
		"diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1,3 +1,3 @@ func x\n a\n-b\n+B\n c\n",
		"--- a/f\n+++ b/f\n@@ -1,2 +1,1 @@\n a\n-b\n\\ No newline at end of file\n",
		"@@ -0,0 +1,2 @@\n+x\n+y\n\\ No newline at end of file\n",
		"@@ -5 +5 @@\n-old\n+new\n@@ -9,2 +9,3 @@\n x\n+y\n \n",
		"@@ -99999999999999999999,1 +1 @@\n-x\n",
	} {
		f.Add(s, 1, 3, false)
		f.Add(s, 2, 2, true)
	}
	f.Fuzz(func(t *testing.T, diff string, from, to int, reverse bool) {
		if p, ok := extractHunkPatch(diff, from); ok && !strings.Contains(p, "@@") {
			t.Fatalf("hunk patch without a hunk: %q", p)
		}
		patch, err := BuildLinePatch(diff, from, to, reverse)
		if err != nil {
			return
		}
		_, hunks := parseFileDiff(patch)
		if len(hunks) == 0 {
			t.Fatalf("patch without hunks: %q", patch)
		}
		for _, h := range hunks {
			oldN, newN := 0, 0
			for _, l := range h.body {
				if l == "" {
					continue
				}
				switch l[0] {
				case ' ':
					oldN++
					newN++
				case '-':
					oldN++
				case '+':
					newN++
				}
			}
			if oldN != h.oldCount || newN != h.newCount {
				t.Fatalf("hunk counts -%d +%d, body has -%d +%d:\n%s", h.oldCount, h.newCount, oldN, newN, patch)
			}
		}
	})
}

// FuzzRebaseLog reads any git log output as a rebase todo. A todo that
// loads is all picks, valid, and formats one line per commit.
func FuzzRebaseLog(f *testing.F) {
	for _, s := range []string{
		"abc\x1fabcdef01\x1fp0\x1ffirst\x1fbody\n\x1e\nabd\x1fabdef02\x1fabcdef01\x1fsecond\x1f\x1e",
		"m\x1fmm\x1fp1 p2\x1fmerge\x1f\x1e",
		"",
		"a\x1fb\x1f\x1fsub\nline\x1f\x1e",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, out string) {
		todo, err := parseRebaseLog(out, "base")
		if err != nil {
			return
		}
		if err := todo.Validate(); err != nil {
			t.Fatalf("all-pick todo not valid: %v", err)
		}
		if todo.Result() != len(todo.Items) {
			t.Fatalf("result %d for %d picks", todo.Result(), len(todo.Items))
		}
		_ = todo.Summary()
		formatted := todo.Format()
		lines := strings.Split(strings.TrimSuffix(formatted, "\n"), "\n")
		clean := true
		for _, it := range todo.Items {
			if strings.ContainsAny(it.FullHash, " \t\r\n") || it.FullHash == "" {
				clean = false
			}
		}
		if clean && len(lines) != len(todo.Items) {
			t.Fatalf("%d items gave %d todo lines:\n%s", len(todo.Items), len(lines), formatted)
		}
	})
}
