package shotkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/mergeview"
	"github.com/amin-jalali/termocode/internal/theme"
)

// The three-way merge view (internal/mergeview) on its own: no editor
// process and no repository, so the frame only changes when the view does.
func TestMergeViewGolden(t *testing.T) {
	oldProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(oldProfile) })
	oldTheme, _ := theme.LookupTheme("")
	theme.ApplyTheme("tokyo-night")
	t.Cleanup(func() { theme.SetPalette(oldTheme.Palette) })

	base := "package main\n\nimport \"fmt\"\n\nfunc greet(name string) string {\n\treturn \"Hello, \" + name\n}\n\nfunc main() {\n\tfmt.Println(greet(\"world\"))\n}\n"
	ours := "package main\n\nimport \"fmt\"\n\nfunc greet(name string) string {\n\treturn \"Hi, \" + name + \"!\"\n}\n\nfunc main() {\n\tfmt.Println(greet(\"team\"))\n}\n"
	theirs := "package main\n\nimport (\n\t\"fmt\"\n\t\"strings\"\n)\n\nfunc greet(name string) string {\n\treturn \"Hello, \" + strings.ToUpper(name)\n}\n\nfunc main() {\n\tfmt.Println(greet(\"world\"))\n\tfmt.Println(greet(\"merge\"))\n}\n"
	result := []string{
		"package main", "",
		"<<<<<<< HEAD", "import \"fmt\"", "=======", "import (", "\t\"fmt\"", "\t\"strings\"", ")", ">>>>>>> feature", "",
		"func greet(name string) string {",
		"<<<<<<< HEAD", "\treturn \"Hi, \" + name + \"!\"", "=======", "\treturn \"Hello, \" + strings.ToUpper(name)", ">>>>>>> feature",
		"}", "", "func main() {",
		"<<<<<<< HEAD", "\tfmt.Println(greet(\"team\"))", "=======", "\tfmt.Println(greet(\"world\"))", "\tfmt.Println(greet(\"merge\"))", ">>>>>>> feature",
		"}",
	}
	st := git.ConflictStages{
		Path:   "main.go",
		Base:   git.ConflictSide{Present: true, Text: base},
		Ours:   git.ConflictSide{Present: true, Text: ours},
		Theirs: git.ConflictSide{Present: true, Text: theirs},
	}
	for _, c := range []struct {
		name string
		w, h int
	}{{"merge-view-150x42", 150, 42}, {"merge-view-80x24", 80, 24}} {
		t.Run(c.name, func(t *testing.T) {
			m := mergeview.New("main.go", st)
			m.SetResult(result)
			m.Next() // the second conflict is current
			m.SetSize(c.w, c.h, c.w < 120)
			m.SetFocused(true)
			frame := m.View() + "\n"
			rows := strings.Split(strings.TrimSuffix(frame, "\n"), "\n")
			if len(rows) != c.h {
				t.Fatalf("rows = %d", len(rows))
			}
			for _, r := range rows {
				if ansi.StringWidth(r) != c.w {
					t.Fatalf("row width %d: %q", ansi.StringWidth(r), ansi.Strip(r))
				}
			}
			path := filepath.Join(goldenDir, c.name+".ans")
			if *update || os.Getenv("UPDATE_GOLDEN") == "1" {
				if err := os.WriteFile(path, []byte(frame), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(want) != frame {
				t.Fatalf("%s differs; run with -update\n%s", c.name, ansi.Strip(frame))
			}
		})
	}
}
