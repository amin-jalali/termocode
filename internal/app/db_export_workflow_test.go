package app_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/testkit"
)

var exportName = regexp.MustCompile(`users-\d{4}-\d{2}-\d{2}\.(csv|tsv|json|sql|md)`)

func frameText(d *testkit.Driver) string { return ansi.Strip(d.Frame()) }

// exportAs opens the data view of users, presses S, picks the format at
// position pos and returns the default file name the prompt offers.
func exportAs(t *testing.T, d *testkit.Driver, pos int) string {
	t.Helper()
	openUsers(t, d)
	d.Key("S")
	if !d.WaitForText("Markdown table", 2*time.Second) {
		t.Fatalf("no export picker:\n%s", frameText(d))
	}
	for _, f := range []string{"CSV", "TSV", "JSON", "SQL INSERTs", "Markdown table"} {
		if !strings.Contains(frameText(d), f) {
			t.Fatalf("picker lacks %s:\n%s", f, frameText(d))
		}
	}
	for i := 0; i < pos; i++ {
		d.Key("down")
	}
	d.Key("enter")
	if !d.WaitForText("File (in the workspace", 2*time.Second) {
		t.Fatalf("no file prompt:\n%s", frameText(d))
	}
	name := exportName.FindString(frameText(d))
	if name == "" {
		t.Fatalf("the prompt has no default name:\n%s", frameText(d))
	}
	return name
}

func waitFile(t *testing.T, d *testkit.Driver, path string) string {
	t.Helper()
	var b []byte
	if !d.WaitFor(func() bool {
		var err error
		b, err = os.ReadFile(path)
		return err == nil
	}, 5*time.Second) {
		t.Fatalf("file %s was not written:\n%s", path, frameText(d))
	}
	return string(b)
}

func TestDBExportCSVWritesTheLoadedRowsAndProtectsFormulas(t *testing.T) {
	s := newEditSession(5)
	s.rows[1][1] = `=HYPERLINK("http://evil")`
	s.rows[2][1] = "a,b"
	d := editDriver(t, 100, 30, s, "")
	name := exportAs(t, d, 0)
	d.Key("enter")
	got := waitFile(t, d, filepath.Join(d.Dir, name))
	want := "id,email,plan\r\n1,user-001@example.com,pro\r\n2,\"'=HYPERLINK(\"\"http://evil\"\")\",team\r\n3,\"a,b\",free\r\n4,user-004@example.com,pro\r\n5,user-005@example.com,team\r\n"
	if got != want {
		t.Fatalf("file:\n%q\nwant:\n%q", got, want)
	}
	if !d.WaitForText("Exported 5 rows", 2*time.Second) {
		t.Fatalf("no toast:\n%s", frameText(d))
	}
}

func TestDBExportFormatsJSONSQLMarkdownTSV(t *testing.T) {
	for _, c := range []struct {
		pos  int
		ext  string
		must []string
	}{
		{1, ".tsv", []string{"id\temail\tplan\r\n", "1\tuser-001@example.com\tpro\r\n"}},
		{2, ".json", []string{`{"id":1,"email":"user-001@example.com","plan":"pro"}`, "[\n", "\n]\n"}},
		{3, ".sql", []string{"INSERT INTO `app`.`users` (`id`, `email`, `plan`) VALUES (2, 'it''s a\\\\b', 'team');\n"}},
		{4, ".md", []string{"| id | email | plan |\n| --- | --- | --- |\n", "| 3 | user-003@example.com | free |\n"}},
	} {
		s := newEditSession(3)
		s.rows[1][1] = `it's a\b`
		d := editDriver(t, 100, 30, s, "")
		name := exportAs(t, d, c.pos)
		if !strings.HasSuffix(name, c.ext) {
			t.Fatalf("default name %q, want %s", name, c.ext)
		}
		d.Key("enter")
		got := waitFile(t, d, filepath.Join(d.Dir, name))
		for _, m := range c.must {
			if !strings.Contains(got, m) {
				t.Fatalf("%s lacks %q:\n%s", c.ext, m, got)
			}
		}
	}
}

func TestDBExportAsksBeforeReplacingAFile(t *testing.T) {
	s := newEditSession(3)
	d := editDriver(t, 100, 30, s, "")
	name := exportAs(t, d, 0)
	path := filepath.Join(d.Dir, name)
	if err := os.WriteFile(path, []byte("precious"), 0o644); err != nil {
		t.Fatal(err)
	}
	d.Key("enter")
	if !d.WaitForText("Replace the existing file?", 2*time.Second) {
		t.Fatalf("no overwrite card:\n%s", frameText(d))
	}
	d.Key("enter") // Cancel is the first button
	d.Settle(200 * time.Millisecond)
	if b, _ := os.ReadFile(path); string(b) != "precious" {
		t.Fatalf("file changed without permission: %q", b)
	}
	// Again, this time replace.
	d.Key("S")
	d.WaitForText("Markdown table", 2*time.Second)
	d.Key("enter")
	d.WaitForText("File (in the workspace", 2*time.Second)
	d.Key("enter")
	d.WaitForText("Replace the existing file?", 2*time.Second)
	d.Key("right", "enter")
	if !d.WaitFor(func() bool { b, _ := os.ReadFile(path); return strings.HasPrefix(string(b), "id,email,plan") }, 3*time.Second) {
		b, _ := os.ReadFile(path)
		t.Fatalf("file not replaced: %q\n%s", b, frameText(d))
	}
}

func TestDBExportRefusesAFolder(t *testing.T) {
	s := newEditSession(3)
	d := editDriver(t, 100, 30, s, "")
	exportAs(t, d, 0)
	for i := 0; i < 40; i++ {
		d.Key("backspace")
	}
	d.Type("subdir")
	if err := os.Mkdir(filepath.Join(d.Dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	d.Key("enter")
	if !d.WaitForText("Cannot export", 2*time.Second) {
		t.Fatalf("no refusal:\n%s", frameText(d))
	}
}

func TestDBExportAllStreamsInPagesFromAReadOnlySession(t *testing.T) {
	s := newEditSession(2500)
	d := editDriver(t, 100, 30, s, "")
	name := exportAs(t, d, 0)
	d.Key("enter")
	if !d.WaitForText("Export all", 2*time.Second) || !strings.Contains(frameText(d), "Export this page") {
		t.Fatalf("no scope card:\n%s", frameText(d))
	}
	d.Key("right", "right", "enter") // Cancel · this page · Export all
	got := waitFile(t, d, filepath.Join(d.Dir, name))
	if lines := strings.Count(got, "\r\n"); lines != 2501 {
		t.Fatalf("%d lines, want 2501 (header + every row)", lines)
	}
	if !strings.Contains(got, "2500,user-2500@example.com") {
		t.Fatalf("last row missing:\n%s", got[len(got)-200:])
	}
	s.mu.Lock()
	ro := append([]string(nil), s.ro...)
	s.mu.Unlock()
	pages := 0
	for _, q := range ro {
		if strings.Contains(q, "termocode_export") {
			pages++
			if !strings.Contains(q, "LIMIT 1000") {
				t.Fatalf("a page without a limit of 1000: %s", q)
			}
		}
	}
	if pages != 3 {
		t.Fatalf("%d export pages through the read-only session, want 3: %q", pages, ro)
	}
	if !d.WaitForText("Exported 2500 rows", 3*time.Second) {
		t.Fatalf("no toast:\n%s", frameText(d))
	}
}

func TestDBExportThisPageOnly(t *testing.T) {
	s := newEditSession(250)
	d := editDriver(t, 100, 30, s, "")
	name := exportAs(t, d, 0)
	d.Key("enter")
	d.WaitForText("Export this page", 2*time.Second)
	d.Key("right", "enter")
	got := waitFile(t, d, filepath.Join(d.Dir, name))
	if lines := strings.Count(got, "\r\n"); lines != 101 {
		t.Fatalf("%d lines, want 101 (header + one page)", lines)
	}
}

// blockExport is a session whose read-only twin never answers a page until
// the context ends, so a cancel can be tried.
type blockExport struct {
	*editSession
	started chan struct{}
	once    sync.Once
}

func (b *blockExport) ReadOnlySession() (dbclient.Session, error) {
	return blockRO{b}, nil
}

type blockRO struct{ b *blockExport }

func (r blockRO) Ping(context.Context) error                 { return nil }
func (r blockRO) Cancel()                                    {}
func (r blockRO) Close() error                               { return nil }
func (r blockRO) Begin(context.Context) error                { return fmt.Errorf("read-only") }
func (r blockRO) Commit(context.Context) error               { return fmt.Errorf("read-only") }
func (r blockRO) Rollback(context.Context) error             { return fmt.Errorf("read-only") }
func (r blockRO) ReadOnlySession() (dbclient.Session, error) { return r, nil }
func (r blockRO) Introspect(ctx context.Context, q dbclient.IntrospectionRequest) (dbclient.IntrospectionResult, error) {
	return r.b.Introspect(ctx, q)
}
func (r blockRO) Run(ctx context.Context, sql string, o dbclient.RunOptions) dbclient.Result {
	if !strings.Contains(sql, "termocode_export") {
		return roEdit{r.b.editSession}.Run(ctx, sql, o)
	}
	r.b.once.Do(func() { close(r.b.started) })
	<-ctx.Done()
	return dbclient.Result{Err: ctx.Err()}
}

func TestDBExportAllCancelWritesNoFile(t *testing.T) {
	b := &blockExport{editSession: newEditSession(250), started: make(chan struct{})}
	d := xferDriver(t, 100, 30, b, "", nil)
	name := exportAs(t, d, 0)
	d.Key("enter")
	d.WaitForText("Export this page", 2*time.Second)
	d.Key("right", "right", "enter")
	select {
	case <-b.started:
	case <-time.After(5 * time.Second):
		t.Fatalf("the export never started:\n%s", frameText(d))
	}
	if !d.WaitForText("Esc cancels", 2*time.Second) || !strings.Contains(frameText(d), "Exporting to "+name) {
		t.Fatalf("no progress card:\n%s", frameText(d))
	}
	d.Key("esc")
	if !d.WaitForText("Export cancelled", 3*time.Second) {
		t.Fatalf("no cancel toast:\n%s", frameText(d))
	}
	entries, _ := os.ReadDir(d.Dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "users-") || strings.Contains(e.Name(), "termocode-export") {
			t.Fatalf("a file was left behind: %s", e.Name())
		}
	}
}

// xferDriver is editDriver for any session and any extra workspace files.
func xferDriver(t *testing.T, w, h int, sess dbclient.Session, conn string, extra map[string]string) *testkit.Driver {
	t.Helper()
	t.Cleanup(app.SetDatabaseSessionFactoryForTest(func(context.Context, dbclient.Conn, string) (dbclient.Session, error) { return sess, nil }))
	if conn == "" {
		conn = `{"id":"local","driver":"mysql","host":"localhost","port":3306}`
	}
	files := map[string]string{
		"README.md":                 "x\n",
		".termocode/databases.json": `{"connections":[` + conn + `]}`,
	}
	for k, v := range extra {
		files[k] = v
	}
	d := testkit.New(t, testkit.Options{W: w, H: h, Nvim: true, Files: files})
	d.Key("f1")
	d.Type("Databases: Focus")
	d.Key("enter")
	if !d.WaitForText("DATABASES", time.Second) {
		t.Fatalf("view did not open:\n%s", frameText(d))
	}
	d.Key("enter", "down", "enter")
	for _, name := range []string{"Tables", "users"} {
		if !d.WaitForText(name, 2*time.Second) {
			t.Fatalf("tree lacks %s:\n%s", name, frameText(d))
		}
	}
	d.Key("down", "down")
	return d
}
