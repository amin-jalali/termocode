package testkit

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// SCN-24: the database connection drops mid-query. A fake sqlite3 on PATH
// prints two rows of "big" and dies. Through the real app and a fake
// model, the AI's db_query hears that the query stopped mid-way with the
// rows read so far and that the result is not complete; the next read on
// the same connection works again (a fresh sqlite3 process).
func TestAIDBConnectionDropMidQuery(t *testing.T) {
	if testing.Short() {
		t.Skip("starts nvim")
	}
	if runtime.GOOS == "windows" {
		t.Skip("fake sqlite3 is a shell script")
	}
	s, srv := fakeToolModel(t,
		toolCall{"db_query", map[string]any{"conn": "lite", "sql": "SELECT n FROM big"}},
		toolCall{"db_query", map[string]any{"conn": "lite", "sql": "SELECT 1 AS ok"}})
	files := textFiles()
	files[".termocode/databases.json"] = `{"connections":[{"id":"lite","driver":"sqlite","path":"app.db"}]}`
	files["app.db"] = ""
	d := New(t, Options{W: 160, H: 40, Nvim: true, Files: files,
		Setup: func(tb testing.TB, _ string) {
			tb.Setenv("TERMOCODE_AI_PROVIDER", "ollama")
			tb.Setenv("TERMOCODE_AI_MODEL", "fake")
			tb.Setenv("TERMOCODE_AI_BASE_URL", srv.URL+"/v1")
			bin := filepath.Join(t.TempDir(), "bin")
			if err := os.MkdirAll(bin, 0o755); err != nil {
				tb.Fatal(err)
			}
			fake := `#!/bin/sh
if [ "$1" = "-version" ]; then echo '3.45.1 2024-01-30'; exit 0; fi
while IFS= read -r line; do
  case "$line" in
    *FROM\ big*) printf '[{"n":1},\n{"n":2},\n'; exit 3 ;;
    SELECT*) echo '[{"ok":1}]' ;;
    .print\ __TC_END_*) echo "${line#*.print }" ;;
  esac
done
`
			if err := os.WriteFile(filepath.Join(bin, "sqlite3"), []byte(fake), 0o755); err != nil {
				tb.Fatal(err)
			}
			tb.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
		}})
	askAI(d, "count the big table")
	httpWaitDone(d)
	if strings.Contains(screen(d), "AI wants to") {
		t.Fatalf("a read asked:\n%s", screen(d))
	}
	r := s.toolResults()
	if len(r) != 2 {
		t.Fatalf("results %q\n%s", r, screen(d))
	}
	for _, want := range []string{"stopped mid-query", "after 2 rows", "not complete"} {
		if !strings.Contains(r[0], want) {
			t.Errorf("dropped read result %q lacks %q", r[0], want)
		}
	}
	if strings.Contains(r[1], "stopped") || !strings.Contains(r[1], "1 row") {
		t.Errorf("read after the drop = %q, want one row", r[1])
	}
}
