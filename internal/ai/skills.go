package ai

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Skills on demand (OM-4): knowledge files the AI loads only when a task
// needs them. A skill is a markdown file with front matter:
//
//	---
//	name: go-tests
//	description: How to run and fix the Go tests in this repo
//	---
//	<body>
//
// The model sees only the names and descriptions (cheap); the skill_load
// tool returns one body. Project skills (<root>/.termocode/skills/*.md)
// come from the repository and are untrusted, as project rules are; user
// skills (<config>/termocode/skills/*.md) are the user's own. Files are
// read when asked (no cache, no watcher).

// SkillsDir is the project skills folder, relative to the workspace root.
const SkillsDir = ".termocode/skills"

// SkillMaxBytes caps one skill file; a bigger file is skipped.
const SkillMaxBytes = 32 << 10

// SkillDescMaxRunes caps a description (it goes into every prompt).
const SkillDescMaxRunes = 160

// SkillsMax caps how many skills are listed (the prompt stays small).
const SkillsMax = 64

// Skill sources.
const (
	SkillSourceProject = "project"
	SkillSourceUser    = "user"
)

// Skill is one loaded skill file.
type Skill struct {
	Name        string
	Description string // one line, cleaned
	Source      string // SkillSourceProject or SkillSourceUser
	Path        string // the file
	Body        string // cleaned text after the front matter
}

// Skills is what LoadSkills found.
type Skills struct {
	List        []Skill  // sorted by name; a name appears once
	Notes       []string // skipped files: "<file>: <why>" (names only, never text)
	ProjectHash string   // sha256 of the project skills ("" when there are none)
}

// Find returns the skill called name.
func (s Skills) Find(name string) (Skill, bool) {
	for _, sk := range s.List {
		if sk.Name == name {
			return sk, true
		}
	}
	return Skill{}, false
}

// Project returns the project skills.
func (s Skills) Project() []Skill {
	var out []Skill
	for _, sk := range s.List {
		if sk.Source == SkillSourceProject {
			out = append(out, sk)
		}
	}
	return out
}

var skillNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ValidSkillName reports whether name can name a skill (letters, digits,
// '.', '_', '-'; at most 64 characters).
func ValidSkillName(name string) bool { return skillNameRe.MatchString(name) }

// LoadSkills reads the user skills of userDir ("" for none) and the
// project skills of root. A user skill keeps its name when a project
// skill has the same one (the user's own file wins over a repository's).
func LoadSkills(root, userDir string) Skills {
	var s Skills
	seen := map[string]string{} // name → file that has it
	if userDir != "" {
		s.load(userDir, userDir, SkillSourceUser, "skills/", seen)
	}
	if root != "" {
		s.load(root, filepath.Join(root, filepath.FromSlash(SkillsDir)), SkillSourceProject, SkillsDir+"/", seen)
	}
	sort.Slice(s.List, func(i, j int) bool { return s.List[i].Name < s.List[j].Name })
	if len(s.List) > SkillsMax {
		for _, sk := range s.List[SkillsMax:] {
			s.Notes = append(s.Notes, filepath.Base(sk.Path)+": more than "+fmt.Sprint(SkillsMax)+" skills")
		}
		s.List = s.List[:SkillsMax]
	}
	h := sha256.New()
	n := 0
	for _, sk := range s.List {
		if sk.Source == SkillSourceProject {
			fmt.Fprintf(h, "%s\n%s\n%s\x00", sk.Name, sk.Description, sk.Body)
			n++
		}
	}
	if n > 0 {
		s.ProjectHash = hex.EncodeToString(h.Sum(nil))
	}
	return s
}

// load adds the *.md files of dir; files must stay inside base (a
// symlink out of the project is skipped). label prefixes the notes.
func (s *Skills) load(base, dir, source, label string, seen map[string]string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	realBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return
	}
	for _, e := range entries {
		file := e.Name()
		if e.IsDir() || strings.HasPrefix(file, ".") || !strings.EqualFold(filepath.Ext(file), ".md") {
			continue
		}
		note := func(why string) { s.Notes = append(s.Notes, label+file+": "+why) }
		text, size, ok := readRulesFile(realBase, filepath.Join(dir, file))
		switch {
		case !ok:
			note("empty or unreadable")
			continue
		case size > SkillMaxBytes:
			note("over 32 KB")
			continue
		}
		name, desc, body, err := ParseSkill(text, file)
		if err != nil {
			note(err.Error())
			continue
		}
		if prev, dup := seen[name]; dup {
			note(fmt.Sprintf("name %q is used by %s", name, prev))
			continue
		}
		seen[name] = label + file
		s.List = append(s.List, Skill{Name: name, Description: desc, Source: source, Path: filepath.Join(dir, file), Body: body})
	}
}

// ParseSkill splits a skill file's text into its name, description and
// body. file is the file name: its stem is the name when the front
// matter has none.
func ParseSkill(text, file string) (name, desc, body string, err error) {
	text = strings.TrimLeft(text, "\ufeff \t\n")
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return "", "", "", errors.New("no front matter (--- name / description ---)")
	}
	var head string
	switch {
	case strings.HasPrefix(rest, "---\n") || rest == "---":
		head, body = "", strings.TrimPrefix(strings.TrimPrefix(rest, "---"), "\n")
	default:
		i := strings.Index(rest, "\n---")
		if i < 0 || (i+4 < len(rest) && rest[i+4] != '\n') {
			return "", "", "", errors.New("no front matter (--- name / description ---)")
		}
		head, body = rest[:i], rest[min(i+5, len(rest)):]
	}
	for _, line := range strings.Split(head, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "name":
			name = v
		case "description":
			desc = strings.Join(strings.Fields(v), " ")
		}
	}
	if name == "" {
		name = strings.TrimSuffix(file, filepath.Ext(file))
	}
	if !ValidSkillName(name) {
		return "", "", "", fmt.Errorf("bad name %q (use letters, digits, . _ -)", name)
	}
	if desc == "" {
		return "", "", "", errors.New("no description in the front matter")
	}
	if r := []rune(desc); len(r) > SkillDescMaxRunes {
		desc = strings.TrimSpace(string(r[:SkillDescMaxRunes-1])) + "…"
	}
	body = strings.Trim(body, " \t\n")
	if body == "" {
		return "", "", "", errors.New("empty body")
	}
	return name, desc, body, nil
}
