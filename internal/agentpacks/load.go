// Package agentpacks installs packs of agents, skills, rules and
// commands into outside agents' own config folders (OMX-46): Claude Code
// (~/.claude) and Codex (~/.codex). Load reads a pack folder (untrusted
// data: no links, size limits); PlanInstall builds the list of files it
// adds or changes and its token cost for the preview card; Apply writes
// it and keeps an undo record outside the agent's folder; Undo puts the
// old files back. Nothing is written without a Plan the user saw.
package agentpacks

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Kind is one part of a pack.
type Kind string

const (
	KindAgent   Kind = "agent"
	KindSkill   Kind = "skill"
	KindRule    Kind = "rule"
	KindCommand Kind = "command"
)

// Kinds are the parts in the order the card lists them.
var Kinds = []Kind{KindAgent, KindSkill, KindRule, KindCommand}

// Limits for a pack (it comes from outside).
const (
	MaxFileBytes  = 1 << 20
	MaxFiles      = 5000
	MaxTotalBytes = 64 << 20
)

// File is one file of a pack.
type File struct {
	Kind Kind
	Rel  string // slash path inside the pack, e.g. "skills/tdd/SKILL.md"
	Data []byte
}

// Pack is a loaded pack.
type Pack struct {
	Name, Version string
	Dir           string
	Source        string // where it came from (the folder, or a git source)
	Commit        string // the git commit it was read at, if any
	Files         []File
}

// parts maps the pack's top folders to kinds.
var parts = []struct {
	dir  string
	kind Kind
}{
	{"agents", KindAgent},
	{"commands", KindCommand},
	{"rules", KindRule},
	{"skills", KindSkill},
}

// Load reads the pack in dir.
func Load(dir string) (Pack, error) {
	p := Pack{Dir: dir, Source: dir}
	p.Name, p.Version = manifest(dir)
	total := 0
	for _, part := range parts {
		root := filepath.Join(dir, part.dir)
		fi, err := os.Lstat(root)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return p, err
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			return p, fmt.Errorf("%s is a link: a pack may not point outside its folder", part.dir)
		}
		if !fi.IsDir() {
			continue
		}
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(dir, path)
			rel = filepath.ToSlash(rel)
			if path != root && strings.HasPrefix(d.Name(), ".") {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.Type()&fs.ModeSymlink != 0 {
				return fmt.Errorf("%s is a link: a pack may not point outside its folder", rel)
			}
			if d.IsDir() || !d.Type().IsRegular() || !wanted(part.kind, rel) {
				return nil
			}
			fi, err := d.Info()
			if err != nil {
				return err
			}
			if fi.Size() > MaxFileBytes {
				return fmt.Errorf("%s is too big (%d KB; the limit is %d KB)", rel, fi.Size()>>10, MaxFileBytes>>10)
			}
			total += int(fi.Size())
			if total > MaxTotalBytes || len(p.Files) >= MaxFiles {
				return fmt.Errorf("the pack is too big (more than %d files or %d MB)", MaxFiles, MaxTotalBytes>>20)
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			p.Files = append(p.Files, File{Kind: part.kind, Rel: rel, Data: b})
			return nil
		})
		if err != nil {
			return p, err
		}
	}
	if len(p.Files) == 0 {
		return p, fmt.Errorf("%s has no agents, skills, rules or commands (folders agents/, skills/, rules/, commands/)", dir)
	}
	sort.Slice(p.Files, func(i, j int) bool { return p.Files[i].Rel < p.Files[j].Rel })
	return p, nil
}

// wanted: agents and commands are .md files right in their folder; rules
// are .md files at any depth; a skill is any file in skills/<name>/.
func wanted(k Kind, rel string) bool {
	seg := strings.Split(rel, "/")
	switch k {
	case KindAgent, KindCommand:
		return len(seg) == 2 && strings.HasSuffix(rel, ".md")
	case KindRule:
		return strings.HasSuffix(rel, ".md")
	case KindSkill:
		return len(seg) >= 3
	}
	return false
}

// manifest reads name and version from pack.json, else package.json,
// else the folder name and "local".
func manifest(dir string) (name, version string) {
	for _, f := range []string{"pack.json", "package.json"} {
		b, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			continue
		}
		var m struct{ Name, Version string }
		if json.Unmarshal(b, &m) == nil && m.Name != "" {
			name, version = m.Name, m.Version
			break
		}
	}
	if name == "" {
		name = filepath.Base(filepath.Clean(dir))
	}
	if version == "" {
		version = "local"
	}
	return name, version
}

// Count is the number of agents, skills (folders), rules and commands.
func (p Pack) Count() map[Kind]int {
	c := map[Kind]int{}
	skills := map[string]bool{}
	for _, f := range p.Files {
		if f.Kind == KindSkill {
			skills[strings.Split(f.Rel, "/")[1]] = true
			continue
		}
		c[f.Kind]++
	}
	c[KindSkill] = len(skills)
	return c
}

// Tokens is a rough token count (about 4 bytes per token).
func Tokens(b []byte) int { return (len(b) + 3) / 4 }

// Tokens is the cost of the parts in kinds (nil: all): always is what
// the agent loads into every chat (rules in full; the name and
// description of agents, skills and commands), onUse is the rest of
// their text, loaded when one is used.
func (p Pack) Tokens(kinds []Kind) (always, onUse int) {
	for _, f := range p.Files {
		if !hasKind(kinds, f.Kind) {
			continue
		}
		a, u := fileTokens(f)
		always += a
		onUse += u
	}
	return always, onUse
}

func fileTokens(f File) (always, onUse int) {
	switch f.Kind {
	case KindRule:
		return Tokens(f.Data), 0
	case KindSkill:
		if !strings.HasSuffix(f.Rel, "/SKILL.md") || strings.Count(f.Rel, "/") != 2 {
			return 0, 0
		}
	}
	head, body := frontmatter(f.Data)
	return Tokens([]byte(head)), Tokens(body)
}

func hasKind(kinds []Kind, k Kind) bool {
	if kinds == nil {
		return true
	}
	for _, x := range kinds {
		if x == k {
			return true
		}
	}
	return false
}

// frontmatter splits a markdown file into "name description" of its
// front matter and the body after it.
func frontmatter(b []byte) (head string, body []byte) {
	s := string(b)
	if !strings.HasPrefix(s, "---\n") && !strings.HasPrefix(s, "---\r\n") {
		return "", b
	}
	rest := s[strings.Index(s, "\n")+1:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", b
	}
	var name, desc string
	for _, line := range strings.Split(rest[:end], "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		switch strings.TrimSpace(k) {
		case "name":
			name = v
		case "description":
			desc = v
		}
	}
	after := rest[end+len("\n---"):]
	if i := strings.Index(after, "\n"); i >= 0 {
		after = after[i+1:]
	} else {
		after = ""
	}
	return strings.TrimSpace(name + " " + desc), bytes.Clone([]byte(after))
}
