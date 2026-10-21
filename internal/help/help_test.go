package help

import (
	"io/fs"
	"os"
	"path"
	"regexp"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/docs"
)

func TestParse(t *testing.T) {
	cases := []struct {
		in   string
		want Loc
		ok   bool
	}{
		{"guides/ai-use.md#chat-panel", Loc{Page: "guides/ai-use.md", Anchor: "chat-panel"}, true},
		{"guides/ai-use.md", Loc{Page: "guides/ai-use.md"}, true},
		{"./guides/../guides/git.md#x", Loc{Page: "guides/git.md", Anchor: "x"}, true},
		{"reference/settings.md#appearance?`theme`", Loc{Page: "reference/settings.md", Anchor: "appearance", Find: "`theme`"}, true},
		{"", Loc{}, false},
		{"#only-anchor", Loc{}, false},
		{"../secret.md", Loc{}, false},
		{"/etc/passwd.md", Loc{}, false},
		{"guides/git", Loc{}, false},
		{"https://example.com/a.md", Loc{}, false},
	}
	for _, c := range cases {
		got, err := Parse(c.in)
		if (err == nil) != c.ok {
			t.Errorf("Parse(%q) err = %v, want ok=%v", c.in, err, c.ok)
			continue
		}
		if c.ok && got != c.want {
			t.Errorf("Parse(%q) = %+v, want %+v", c.in, got, c.want)
		}
		if c.ok && got.String() != strings.TrimPrefix(path.Clean(c.in), "./") && !strings.Contains(c.in, "..") {
			t.Errorf("Loc.String() = %q for %q", got.String(), c.in)
		}
	}
}

func TestRelLink(t *testing.T) {
	cases := []struct {
		from, dest string
		want       Loc
		ok         bool
	}{
		{"guides/git.md", "../reference/keys.md#file", Loc{Page: "reference/keys.md", Anchor: "file"}, true},
		{"guides/git.md", "ai-use.md", Loc{Page: "guides/ai-use.md"}, true},
		{"guides/git.md", "#stage-one-hunk", Loc{Page: "guides/git.md", Anchor: "stage-one-hunk"}, true},
		{"index.md", "getting-started/install.md", Loc{Page: "getting-started/install.md"}, true},
		{"guides/git.md", "../adr/0001-embedded-neovim-editor-engine.md", Loc{Page: "adr/0001-embedded-neovim-editor-engine.md"}, true},
		{"guides/git.md", "../../README.md", Loc{}, false},
		{"guides/git.md", "https://example.com/x.md", Loc{}, false},
		{"guides/git.md", "mailto:a@b.c", Loc{}, false},
		{"guides/git.md", "javascript:alert(1)", Loc{}, false},
		{"guides/git.md", "file:///etc/passwd", Loc{}, false},
		{"guides/git.md", "../assets/screenshots/demo.gif", Loc{}, false},
		{"guides/git.md", "/guides/ai-use.md", Loc{}, false},
		{"guides/git.md", "", Loc{}, false},
		{"guides/git.md", "../concepts/", Loc{}, false},
	}
	for _, c := range cases {
		got, ok := RelLink(c.from, c.dest)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("RelLink(%q, %q) = %+v, %v; want %+v, %v", c.from, c.dest, got, ok, c.want, c.ok)
		}
	}
}

func TestSiteURL(t *testing.T) {
	cases := []struct {
		l    Loc
		want string
	}{
		{Loc{Page: "guides/ai-use.md", Anchor: "chat-panel"}, SiteBase + "guides/ai-use/#chat-panel"},
		{Loc{Page: "guides/ai-use.md"}, SiteBase + "guides/ai-use/"},
		{Loc{Page: "index.md"}, SiteBase},
		{Loc{Page: "reference/index.md", Anchor: "x"}, SiteBase + "reference/#x"},
		{Loc{Page: "adr/README.md"}, SiteBase + "adr/"},
	}
	for _, c := range cases {
		if got := SiteURL(c.l); got != c.want {
			t.Errorf("SiteURL(%+v) = %q, want %q", c.l, got, c.want)
		}
	}
}

// SiteBase is the site_url in mkdocs.yml.
func TestSiteBaseMatchesMkdocs(t *testing.T) {
	b, err := os.ReadFile("../../mkdocs.yml")
	if err != nil {
		t.Skip("not in the repository")
	}
	m := regexp.MustCompile(`(?m)^site_url:\s*(\S+)`).FindSubmatch(b)
	if m == nil || string(m[1]) != SiteBase {
		t.Errorf("mkdocs.yml site_url = %q, SiteBase = %q", m, SiteBase)
	}
	if !strings.Contains(string(b), "exclude_docs: |\n  /embed.go\n") {
		t.Errorf("mkdocs.yml must exclude docs/embed.go from the site")
	}
}

// The embed pattern in docs/embed.go covers every page in the embedded
// folders: a new guide is never left out of the app.
func TestEmbedCoversEveryPage(t *testing.T) {
	for _, dir := range []string{"guides", "reference", "getting-started"} {
		ents, err := os.ReadDir("../../docs/" + dir)
		if err != nil {
			t.Skip("not in the repository")
		}
		for _, e := range ents {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			p := dir + "/" + e.Name()
			if !Has(p) {
				t.Errorf("%s is not embedded (docs/embed.go)", p)
			}
		}
	}
	for _, p := range []string{"index.md", "features.md", "extensions.md"} {
		if !Has(p) {
			t.Errorf("%s is not embedded", p)
		}
	}
	for _, p := range []string{"adr/README.md", "concepts/how-it-works.md", "embed.go", "../go.mod"} {
		if Has(p) {
			t.Errorf("%s should not be embedded", p)
		}
	}
}

// Pages lists every embedded page once, in the site's nav order with the
// nav titles.
func TestPagesMatchNav(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range Pages() {
		if seen[p.Path] {
			t.Errorf("%s listed twice", p.Path)
		}
		seen[p.Path] = true
		if !Has(p.Path) {
			t.Errorf("Pages lists %s, which is not embedded", p.Path)
		}
		if p.Title == "" || p.Section == "" {
			t.Errorf("%s: empty title or section", p.Path)
		}
	}
	_ = fs.WalkDir(docs.FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && !seen[p] {
			t.Errorf("%s is embedded but not in Pages()", p)
		}
		return nil
	})
	b, err := os.ReadFile("../../mkdocs.yml")
	if err != nil {
		t.Skip("not in the repository")
	}
	// "      - Use AI: guides/ai-use.md" or "      - reference/index.md"
	re := regexp.MustCompile(`^[ \t]*- (?:"?([^"\n]+?)"?:[ \t]*)?([\w./-]+\.md)[ \t]*$`)
	var nav []PageInfo
	for _, line := range strings.Split(string(b), "\n") {
		m := re.FindStringSubmatch(line)
		if m != nil && Has(m[2]) {
			nav = append(nav, PageInfo{Path: m[2], Title: m[1]})
		}
	}
	got := Pages()
	if len(nav) != len(got) {
		t.Fatalf("nav has %d embedded pages, Pages() %d", len(nav), len(got))
	}
	for i := range nav {
		if nav[i].Path != got[i].Path {
			t.Errorf("order %d: nav %s, Pages %s", i, nav[i].Path, got[i].Path)
		}
		if nav[i].Title != "" && nav[i].Title != got[i].Title {
			t.Errorf("%s: nav title %q, Pages %q", got[i].Path, nav[i].Title, got[i].Title)
		}
	}
}

func TestLoad(t *testing.T) {
	p, err := Load("guides/ai-use.md")
	if err != nil {
		t.Fatal(err)
	}
	if p.Path != "guides/ai-use.md" || p.Title != "Use AI" {
		t.Errorf("page = %q %q", p.Path, p.Title)
	}
	if len(p.Headings) < 5 || p.Headings[0].Level != 1 || p.Headings[0].Slug != "use-ai" {
		t.Fatalf("headings = %+v", p.Headings)
	}
	q, err := Load("./guides/../guides/ai-use.md")
	if err != nil || q != p {
		t.Errorf("Load is not cached by clean path: %v", err)
	}
	for _, bad := range []string{"../go.mod", "/guides/ai-use.md", "guides/nope.md", "adr/README.md", "", "guides"} {
		if _, err := Load(bad); err == nil {
			t.Errorf("Load(%q) works, want an error", bad)
		}
	}
	idx, err := Load("index.md")
	if err != nil {
		t.Fatal(err)
	}
	if idx.Title != "Home" || idx.Headings[0].Text != "The AI-Integrated Terminal IDE." {
		t.Errorf("index: title %q, first heading %+v", idx.Title, idx.Headings[0])
	}
}

// Heading text is what the site slugs: code spans included, markup
// removed.
func TestHeadingText(t *testing.T) {
	got := headings("# A `b.c` **d** [e](x.md) _f_\n\n```\n# not\n```\n\n> ## In a quote\n\n- ### In a list\n\nSetext\n---\n")
	want := []Heading{
		{1, "A b.c d e f", "a-bc-d-e-f"},
		{2, "In a quote", "in-a-quote"},
		{3, "In a list", "in-a-list"},
		{2, "Setext", "setext"},
	}
	if len(got) != len(want) {
		t.Fatalf("headings = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("heading %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestResolve(t *testing.T) {
	p, h, err := Resolve(Loc{Page: "guides/ai-use.md", Anchor: "chat-panel"})
	if err != nil || p.Path != "guides/ai-use.md" || h.Text != "Chat panel" || h.Level != 2 {
		t.Fatalf("Resolve = %v %+v %v", p, h, err)
	}
	if _, h, err := Resolve(Loc{Page: "guides/ai-use.md"}); err != nil || h != (Heading{}) {
		t.Errorf("no anchor: %+v %v", h, err)
	}
	if _, _, err := Resolve(Loc{Page: "guides/ai-use.md", Anchor: "chat-panels"}); err == nil {
		t.Errorf("a missing anchor resolves")
	}
	if _, _, err := Resolve(Loc{Page: "guides/nope.md"}); err == nil {
		t.Errorf("a missing page resolves")
	}
}

// Check is what the app's mapping tests call for every topic: page,
// anchor and find text must exist. A bad entry must fail.
func TestCheck(t *testing.T) {
	for _, bad := range []string{
		"guides/ai-use.md#chat-panels",
		"guides/nope.md#x",
		"guides/ai-use.md#chat-panel?no such text anywhere",
		"guides/ai-use.md#files-in-and-out-of-the-chat?Chat panel", // text is above the anchor
		"../go.mod",
		"",
	} {
		if err := Check(bad); err == nil {
			t.Errorf("Check(%q) passes, want an error", bad)
		}
	}
	if err := Check("reference/settings.md#appearance?`theme`"); err != nil {
		t.Errorf("Check: %v", err)
	}
}

// The topics the contextual help plan (U21) points at. Every one must
// resolve against the embedded pages; a renamed heading fails here.
var plannedTopics = []string{
	"guides/ai-use.md#chat-panel",
	"guides/ai-use.md#files-in-and-out-of-the-chat",
	"guides/ai-use.md#review-ai-changes",
	"guides/ai-use.md#agent-let-the-chat-use-tools",
	"guides/ai-profiles.md#pick-a-profile",
	"guides/ai-profiles.md#the-agents-panel",
	"guides/editing.md#terminal",
	"guides/editing.md#tabs-and-splits",
	"guides/editing.md#open-files",
	"guides/editing.md#markdown-zen-and-more",
	"guides/editing.md#edit-text",
	"guides/language-servers.md#install-a-language-server",
	"guides/language-servers.md#which-tools-are-supported",
	"guides/tasks-problems.md#the-problems-panel",
	"guides/tasks-problems.md#run-a-task",
	"guides/tests.md#read-a-failure",
	"guides/tests.md#open-the-testing-view",
	"guides/services.md#the-view",
	"guides/services.md#keys",
	"guides/containers.md#the-view",
	"guides/containers.md#keys",
	"guides/containers.md#the-help-section",
	"guides/containers.md#the-ai-and-host-containers",
	"guides/debug.md#step-through-code",
	"guides/debug.md#debug-in-five-steps",
	"guides/debug.md#debug-console",
	"guides/git.md#branches-history-and-stash",
	"guides/git.md#open-the-source-control-panel",
	"guides/git.md#sync-with-the-remote",
	"guides/git.md#stage-one-hunk",
	"guides/git.md#the-commit-view",
	"guides/search.md#find-in-this-file",
	"guides/search.md#replace-in-this-file",
	"guides/search.md#live-overlay",
	"guides/mcp.md#connect-a-service",
	"guides/http-client.md#the-response-page",
	"guides/http-client.md#send-a-request",
	"reference/keys.md#change-a-key",
	"guides/customize.md#pick-a-theme",
	"getting-started/first-steps.md#1-open-a-project",
}

func TestPlannedTopicsResolve(t *testing.T) {
	if errs := CheckAll(plannedTopics); len(errs) > 0 {
		for _, e := range errs {
			t.Error(e)
		}
	}
	if errs := CheckAll(append([]string{"guides/git.md#no-such-heading"}, plannedTopics...)); len(errs) != 1 {
		t.Errorf("CheckAll found %d errors for one bad topic", len(errs))
	}
}
