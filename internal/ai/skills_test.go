package ai

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Spec (OM-4): skills are markdown files with a name and a one-line
// description in front matter, in <root>/.termocode/skills and in the
// user's skills folder. Bad files are skipped with a note.

func writeSkill(t *testing.T, dir, file, text string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, file), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseSkill(t *testing.T) {
	cases := []struct {
		name, text, file string
		wantName         string
		wantDesc         string
		wantBody         string
		wantErr          string
	}{
		{name: "full", file: "x.md", text: "---\nname: go-tests\ndescription: How to run the Go tests\n---\nRun go test -race ./...\n",
			wantName: "go-tests", wantDesc: "How to run the Go tests", wantBody: "Run go test -race ./..."},
		{name: "quoted values and extra keys", file: "x.md", text: "---\nname: \"db\"\ndescription: 'Query the DB'\nversion: 2\n---\n\nbody\n",
			wantName: "db", wantDesc: "Query the DB", wantBody: "body"},
		{name: "name from the file", file: "release-notes.md", text: "---\ndescription: Write release notes\n---\nsteps\n",
			wantName: "release-notes", wantDesc: "Write release notes", wantBody: "steps"},
		{name: "no front matter", file: "x.md", text: "just text\n", wantErr: "no front matter"},
		{name: "front matter not closed", file: "x.md", text: "---\nname: a\ndescription: b\n", wantErr: "no front matter"},
		{name: "no description", file: "x.md", text: "---\nname: a\n---\nbody\n", wantErr: "no description"},
		{name: "bad name", file: "x.md", text: "---\nname: ../evil\ndescription: d\n---\nbody\n", wantErr: "bad name"},
		{name: "empty body", file: "x.md", text: "---\nname: a\ndescription: d\n---\n \n", wantErr: "empty"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			name, desc, body, err := ParseSkill(c.text, c.file)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("err %v, want %q", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if name != c.wantName || desc != c.wantDesc || body != c.wantBody {
				t.Errorf("got %q %q %q", name, desc, body)
			}
		})
	}
}

// Spec: a long description is cut to one short line.
func TestParseSkillLongDescription(t *testing.T) {
	_, desc, _, err := ParseSkill("---\nname: a\ndescription: "+strings.Repeat("w", 500)+"\n---\nbody\n", "a.md")
	if err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(desc)); n > SkillDescMaxRunes || !strings.HasSuffix(desc, "…") {
		t.Errorf("desc has %d runes: %q", n, desc)
	}
}

func TestLoadSkills(t *testing.T) {
	t.Run("nothing", func(t *testing.T) {
		s := LoadSkills(t.TempDir(), t.TempDir())
		if len(s.List) != 5 || len(s.Notes) != 0 || s.ProjectHash != "" {
			t.Errorf("skills %+v", s)
		}
	})

	t.Run("project and user, sorted, with sources", func(t *testing.T) {
		root, user := t.TempDir(), t.TempDir()
		pdir := filepath.Join(root, ".termocode", "skills")
		writeSkill(t, pdir, "b.md", "---\nname: beta\ndescription: B\n---\nbody b\n")
		writeSkill(t, pdir, "notes.txt", "not a skill")
		writeSkill(t, user, "a.md", "---\nname: alpha\ndescription: A\n---\nbody a\n")
		s := LoadSkills(root, user)
		if len(s.List) != 7 || s.List[0].Name != "alpha" || s.List[1].Name != "beta" {
			t.Fatalf("list %+v", s.List)
		}
		if s.List[0].Source != SkillSourceUser || s.List[1].Source != SkillSourceProject {
			t.Errorf("sources %q %q", s.List[0].Source, s.List[1].Source)
		}
		if s.List[1].Path != filepath.Join(pdir, "b.md") || s.List[1].Body != "body b" {
			t.Errorf("skill %+v", s.List[1])
		}
		if s.ProjectHash == "" || len(s.Notes) != 0 {
			t.Errorf("hash %q notes %v", s.ProjectHash, s.Notes)
		}
		if _, ok := s.Find("beta"); !ok {
			t.Error("Find beta")
		}
		if got := len(s.Project()); got != 1 {
			t.Errorf("project skills %d", got)
		}
	})

	t.Run("project hash follows the content", func(t *testing.T) {
		root := t.TempDir()
		pdir := filepath.Join(root, ".termocode", "skills")
		writeSkill(t, pdir, "b.md", "---\nname: beta\ndescription: B\n---\nbody b\n")
		h1 := LoadSkills(root, "").ProjectHash
		writeSkill(t, pdir, "b.md", "---\nname: beta\ndescription: B\n---\nbody changed\n")
		if h2 := LoadSkills(root, "").ProjectHash; h2 == h1 || h2 == "" {
			t.Errorf("hash did not change: %q %q", h1, h2)
		}
	})

	t.Run("bad files are skipped with a note", func(t *testing.T) {
		root, user := t.TempDir(), t.TempDir()
		pdir := filepath.Join(root, ".termocode", "skills")
		writeSkill(t, pdir, "big.md", "---\nname: big\ndescription: d\n---\n"+strings.Repeat("x\n", SkillMaxBytes))
		writeSkill(t, pdir, "bad.md", "no front matter\n")
		writeSkill(t, pdir, "ok.md", "---\nname: ok\ndescription: d\n---\nbody\n")
		writeSkill(t, pdir, "zdup.md", "---\nname: ok\ndescription: again\n---\nbody\n")
		writeSkill(t, user, "mine.md", "---\nname: mine\ndescription: d\n---\nuser body\n")
		writeSkill(t, pdir, "mine.md", "---\nname: mine\ndescription: d\n---\nproject body\n")
		s := LoadSkills(root, user)
		if len(s.List) != 7 {
			t.Fatalf("list %+v", s.List)
		}
		if sk, _ := s.Find("mine"); sk.Source != SkillSourceUser || sk.Body != "user body" {
			t.Errorf("a project skill replaced the user's: %+v", sk)
		}
		notes := strings.Join(s.Notes, "\n")
		for _, want := range []string{"big.md: over 32 KB", "bad.md: no front matter", "zdup.md: name \"ok\" is used", "mine.md: name \"mine\" is used"} {
			if !strings.Contains(notes, want) {
				t.Errorf("notes lack %q:\n%s", want, notes)
			}
		}
	})

	t.Run("a symlink out of the project is skipped", func(t *testing.T) {
		root, out := t.TempDir(), t.TempDir()
		writeSkill(t, out, "secret.md", "---\nname: s\ndescription: d\n---\nbody\n")
		pdir := filepath.Join(root, ".termocode", "skills")
		if err := os.MkdirAll(pdir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(out, "secret.md"), filepath.Join(pdir, "s.md")); err != nil {
			t.Skip(err)
		}
		if s := LoadSkills(root, ""); len(s.List) != 5 {
			t.Errorf("followed the symlink: %+v", s.List)
		}
	})

	t.Run("escape sequences are removed", func(t *testing.T) {
		root := t.TempDir()
		writeSkill(t, filepath.Join(root, ".termocode", "skills"), "e.md", "---\nname: e\ndescription: d\x1b[31mred\n---\nbo\u202edy\n")
		sk, ok := LoadSkills(root, "").Find("e")
		if !ok || sk.Description != "dred" || sk.Body != "body" {
			t.Errorf("skill %+v", sk)
		}
	})
}

func TestBundledSkillsAndOverrides(t *testing.T) {
	root, user := t.TempDir(), t.TempDir()
	s := LoadSkills(root, user)
	for _, name := range []string{"git-commit", "review-work", "remove-ai-slop", "init-agents-md", "debugging"} {
		sk, ok := s.Find(name)
		if !ok || sk.Source != "builtin" || sk.Body == "" || !strings.HasSuffix(sk.Path, "/SKILL.md") {
			t.Errorf("missing built-in %s", name)
		}
	}
	if s.ProjectHash != "" {
		t.Fatal("built-ins require project trust")
	}
	writeSkill(t, user, "git-commit.md", "---\nname: git-commit\ndescription: Mine\n---\nMy workflow\n")
	sk, _ := LoadSkills(root, user).Find("git-commit")
	if sk.Source != SkillSourceUser || sk.Body != "My workflow" {
		t.Fatal("user override lost")
	}
}
