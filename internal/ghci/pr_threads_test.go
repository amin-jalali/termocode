package ghci

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const threadsJSON = `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[
 {"id":"PRRT_1","isResolved":false,"isOutdated":false,"path":"parser.go","line":12,"originalLine":10,"diffSide":"RIGHT",
  "comments":{"nodes":[
   {"databaseId":501,"author":{"login":"bob"},"body":"Why a map?","createdAt":"2026-10-28T10:00:00Z","diffHunk":"@@ -1,3 +1,3 @@\n package p\n-func Old() {}\n+func New() {}","url":"https://github.com/o/r/pull/14#discussion_r501"},
   {"databaseId":502,"author":{"login":"ann"},"body":"Faster lookups.","createdAt":"2026-10-28T11:00:00Z","diffHunk":"","url":"u502"}]}},
 {"id":"PRRT_2","isResolved":true,"isOutdated":true,"path":"docs/a.md","line":null,"originalLine":3,"diffSide":"LEFT",
  "comments":{"nodes":[{"databaseId":600,"author":null,"body":"typo","createdAt":"2026-10-27T09:00:00Z","diffHunk":"","url":"u600"}]}}
]}}}}}`

// Threads reads the review threads through gh's GraphQL call with the
// repository filled in by gh ({owner}/{repo}) and the number typed.
func TestThreads(t *testing.T) {
	f := &fakeGH{answers: map[string]fakeAnswer{"api graphql": {out: threadsJSON}}}
	ts, err := Threads(context.Background(), f.run, "/r", 14)
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 2 {
		t.Fatalf("threads %+v", ts)
	}
	a := ts[0]
	if a.ID != "PRRT_1" || a.Resolved || a.Outdated || a.Path != "parser.go" || a.Line != 12 || a.Side != "RIGHT" ||
		len(a.Comments) != 2 || a.Comments[0].ID != 501 || a.Comments[0].Author != "bob" || a.Comments[1].Body != "Faster lookups." ||
		!strings.Contains(a.Hunk, "+func New() {}") || a.URL != "https://github.com/o/r/pull/14#discussion_r501" {
		t.Errorf("thread 1 %+v", a)
	}
	b := ts[1]
	if !b.Resolved || !b.Outdated || b.Line != 3 || b.Comments[0].Author != "ghost" {
		t.Errorf("thread 2 (outdated: the original line) %+v", b)
	}
	args := strings.Join(f.calls[0], " ")
	for _, want := range []string{"api graphql", "-F owner={owner}", "-F name={repo}", "-F number=14", "reviewThreads"} {
		if !strings.Contains(args, want) {
			t.Errorf("args %q miss %q", args, want)
		}
	}
	if ThreadsOpen(ts) != 1 {
		t.Errorf("open %d", ThreadsOpen(ts))
	}
}

func TestThreadsError(t *testing.T) {
	f := &fakeGH{answers: map[string]fakeAnswer{"api graphql": {errOut: "HTTP 401: Bad credentials", err: errString("exit 1")}}}
	if _, err := Threads(context.Background(), f.run, "/r", 14); err == nil {
		t.Error("no error")
	}
	f = &fakeGH{answers: map[string]fakeAnswer{"api graphql": {out: `{"errors":[{"message":"Could not resolve to a PullRequest"}]}`}}}
	if _, err := Threads(context.Background(), f.run, "/r", 14); err == nil || !strings.Contains(err.Error(), "Could not resolve") {
		t.Errorf("GraphQL error not shown: %v", err)
	}
}

// Reply answers the first comment of a thread; the text is one argument
// read as plain text (-f, so a leading @ is never a file).
func TestReply(t *testing.T) {
	f := &fakeGH{answers: map[string]fakeAnswer{"api --method": {}}}
	if err := Reply(context.Background(), f.run, "/r", 14, 501, " @file sure "); err != nil {
		t.Fatal(err)
	}
	got := f.calls[0]
	want := []string{"api", "--method", "POST", "repos/{owner}/{repo}/pulls/14/comments/501/replies", "-f", "body=@file sure"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("args %q want %q", got, want)
	}
	if err := Reply(context.Background(), (&fakeGH{}).run, "/r", 14, 501, "  "); !errors.Is(err, ErrNeedsText) {
		t.Errorf("empty reply: %v", err)
	}
	if err := Reply(context.Background(), (&fakeGH{}).run, "/r", 14, 0, "x"); err == nil {
		t.Error("no comment id accepted")
	}
}

// SetResolved resolves or reopens a thread by its id.
func TestSetResolved(t *testing.T) {
	for _, c := range []struct {
		resolved bool
		want     string
	}{{true, "resolveReviewThread"}, {false, "unresolveReviewThread"}} {
		f := &fakeGH{answers: map[string]fakeAnswer{"api graphql": {out: `{"data":{}}`}}}
		if err := SetResolved(context.Background(), f.run, "/r", "PRRT_1", c.resolved); err != nil {
			t.Fatal(err)
		}
		args := strings.Join(f.calls[0], " ")
		if !strings.Contains(args, c.want+"(input:{threadId:$id})") || !strings.Contains(args, "-f id=PRRT_1") {
			t.Errorf("args %q", args)
		}
		if c.resolved && strings.Contains(args, "unresolve") {
			t.Errorf("resolve sent unresolve: %q", args)
		}
	}
	if err := SetResolved(context.Background(), (&fakeGH{}).run, "/r", "", true); err == nil {
		t.Error("empty thread id accepted")
	}
}
