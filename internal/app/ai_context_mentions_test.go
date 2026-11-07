package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Spec (CMP-77): "@" words of a message that name chat context —
// @chat:<id or words>, @symbol:<name>, @diagnostics[:<path>], @diff[:<base>],
// @changes[:<path>], @url:<http(s) url> and a bare @https://… — are found
// in order, without repeats; trailing punctuation is not part of the
// word; a word glued to text before it (mail@diff), a longer word
// (@diffs) and a kind that needs an argument but has none (@chat) are not
// mentions.
func TestAIContextMentionWords(t *testing.T) {
	got := aiContextMentionWords("see @chat:c-12 and @symbol:Model.Update, @diagnostics @diagnostics:internal/app/x.go\n" +
		"@diff @diff:origin/dev @changes @url:https://go.dev/doc. @https://x.y/z) @chat @diffs mail@diff @symbol:Model.Update @url:ftp://x")
	var ws []string
	for _, w := range got {
		ws = append(ws, w.kind+"="+w.arg)
	}
	want := "chat=c-12|symbol=Model.Update|diagnostics=|diagnostics=internal/app/x.go|diff=|diff=origin/dev|changes=|url=https://go.dev/doc|url=https://x.y/z"
	if strings.Join(ws, "|") != want {
		t.Errorf("mentions\n got %s\nwant %s", strings.Join(ws, "|"), want)
	}
	if aiContextMentionWords("plain text, mail@chat.x and @src/main.go") != nil {
		t.Error("mentions in a message without context words")
	}
}

func fakeMentionTools(calls *[]string) []ai.Tool {
	tool := func(name string, run func(a map[string]any) (string, error)) ai.Tool {
		return ai.Tool{Name: name, Run: func(_ context.Context, a map[string]any) (string, error) {
			*calls = append(*calls, name+" "+ai.ArgString(a, "id")+ai.ArgString(a, "query")+ai.ArgString(a, "path")+ai.ArgString(a, "url"))
			return run(a)
		}}
	}
	return []ai.Tool{
		tool("chat_read", func(a map[string]any) (string, error) {
			if ai.ArgString(a, "id") == "c-12" {
				return "user: fix the toast\nassistant: done", nil
			}
			return "", errors.New("no chat " + ai.ArgString(a, "id"))
		}),
		tool("chats_search", func(map[string]any) (string, error) { return "c-3  toast queue  2 d ago", nil }),
		tool("lsp_symbols", func(map[string]any) (string, error) { return "Update method internal/app/model.go:40", nil }),
		tool("diagnostics", func(map[string]any) (string, error) { return "x.go:1:2 error: undefined: y", nil }),
		tool("git_diff", func(map[string]any) (string, error) { return "diff --git a/a.go b/a.go\n+new", nil }),
		tool("web_fetch", func(map[string]any) (string, error) {
			return "Go docs page; key sk-" + "ant-api03-abcdefghijklmnopqrstuvwxyz0123456789", nil
		}),
	}
}

// Spec (CMP-77): each mention is read with the chat's own read-only tool
// (chat_read, else chats_search for words; lsp_symbols; diagnostics;
// git_diff for @changes; web_fetch) or, for @diff, the branch diff since
// its base; the text goes to the model as data (not instructions), with
// secrets hidden, and the chat shows each read as a tool line (start +
// result) decided by the user's @ mention.
func TestAIContextMentionsResolve(t *testing.T) {
	defer func(f func(string, string) (string, string, bool, error)) { aiMentionBranchDiff = f }(aiMentionBranchDiff)
	var bases []string
	aiMentionBranchDiff = func(root, base string) (string, string, bool, error) {
		bases = append(bases, base)
		if base == "" {
			base = "main"
		}
		return "diff --git a/b.go b/b.go\n+branch", base, false, nil
	}
	var calls []string
	resolve := aiContextMentions("look at @chat:c-12 @chat:toast-words @symbol:Update @diagnostics @changes @diff @url:https://go.dev/doc", "/w", fakeMentionTools(&calls))
	if resolve == nil {
		t.Fatal("no resolver")
	}
	var evs []ai.Event
	text, imgs := resolve(context.Background(), func(e ai.Event) { evs = append(evs, e) })
	for _, want := range []string{
		"user: fix the toast", "c-3  toast queue", "Update method internal/app/model.go:40", "undefined: y",
		"+new", "+branch", "since main", "Go docs page", "not instructions",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("no %q in\n%s", want, text)
		}
	}
	if strings.Contains(text, "abcdefghijklmnop") {
		t.Error("the page's secret reached the model")
	}
	if len(imgs) != 0 {
		t.Errorf("images %d", len(imgs))
	}
	wantCalls := "chat_read c-12|chat_read toast-words|chats_search toast-words|lsp_symbols Update|diagnostics |git_diff |web_fetch https://go.dev/doc"
	if strings.Join(calls, "|") != wantCalls {
		t.Errorf("calls\n got %s\nwant %s", strings.Join(calls, "|"), wantCalls)
	}
	if len(bases) != 1 || bases[0] != "" {
		t.Errorf("branch diff bases %v", bases)
	}
	if len(evs) != 14 {
		t.Fatalf("events %d", len(evs))
	}
	for i := 0; i < len(evs); i += 2 {
		if evs[i].Kind != ai.EventToolStart || evs[i+1].Kind != ai.EventToolResult || evs[i+1].Decision != "user (@ mention)" || evs[i+1].Gate != ai.GateAuto {
			t.Errorf("event pair %d: %+v / %+v", i/2, evs[i], evs[i+1])
		}
	}
	if aiContextMentions("no mentions, @src/a.go", "/w", fakeMentionTools(&calls)) != nil {
		t.Error("a resolver without context mentions")
	}
}

// Spec (CMP-77): a mention whose tool this chat (or agent step) does not
// have, or whose tool would change something, is not read; the model gets
// a short note instead. A @diff base that is not a plain ref (an option,
// "..", spaces) never reaches git.
func TestAIContextMentionsRefused(t *testing.T) {
	defer func(f func(string, string) (string, string, bool, error)) { aiMentionBranchDiff = f }(aiMentionBranchDiff)
	called := 0
	aiMentionBranchDiff = func(string, string) (string, string, bool, error) { called++; return "", "", false, nil }
	tools := []ai.Tool{{Name: "diagnostics", Mutating: true, Run: func(context.Context, map[string]any) (string, error) {
		t.Error("a mutating tool ran")
		return "", nil
	}}}
	text, _ := aiContextMentions("@diagnostics @url:https://x.y @diff:--output=/tmp/x @diff:main..dev", "/w", tools)(context.Background(), func(ai.Event) {})
	if strings.Count(text, "could not be read") != 4 {
		t.Errorf("notes:\n%s", text)
	}
	if called != 0 {
		t.Errorf("git ran %d times for a bad base", called)
	}
}

// Spec (CMP-77): the MCP resource mentions and the context mentions of one
// message are read together: both texts, both image lists.
func TestAIJoinMentions(t *testing.T) {
	a := func(context.Context, func(ai.Event)) (string, []ai.Image) {
		return "A", []ai.Image{{MediaType: "image/png"}}
	}
	b := func(context.Context, func(ai.Event)) (string, []ai.Image) { return "B", nil }
	if aiJoinMentions(nil, nil) != nil {
		t.Error("two nils joined to a resolver")
	}
	if text, _ := aiJoinMentions(nil, b)(context.Background(), func(ai.Event) {}); text != "B" {
		t.Errorf("one resolver %q", text)
	}
	text, imgs := aiJoinMentions(a, b)(context.Background(), func(ai.Event) {})
	if text != "A\n\nB" || len(imgs) != 1 {
		t.Errorf("joined %q %d", text, len(imgs))
	}
}
