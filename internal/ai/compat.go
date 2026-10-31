package ai

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/amin-jalali/termocode/internal/agentdag"
)

// Setups from other apps (OMX-11): agent files and command files in the
// folders other apps use. A project's files are untrusted, as project
// rules and skills are: agent profiles get the project profile card,
// project commands the skills card (their hash joins the skills hash).

// AgentFilesDir and CommandFilesDir are relative to the workspace root.
const (
	AgentFilesDir   = ".claude/agents"
	CommandFilesDir = ".claude/commands"
)

// agentReadTools are the other apps' tool names that only read.
var agentReadTools = map[string]bool{"read": true, "grep": true, "glob": true, "ls": true,
	"webfetch": true, "websearch": true, "notebookread": true, "todowrite": true, "todoread": true}

// LoadAgentProfiles reads <root>/.claude/agents/*.md as one-step project
// profiles. A file is skipped (with a note) when it is broken or its name
// is a built-in profile's.
func LoadAgentProfiles(root string) ([]Profile, []string) {
	var out []Profile
	var notes []string
	taken := map[string]bool{}
	for _, p := range BuiltinProfiles() {
		taken[p.Name] = true
	}
	for _, f := range compatFiles(root, AgentFilesDir, &notes) {
		head, body, err := splitFrontMatter(f.text)
		if err != nil {
			notes = append(notes, f.label+": "+err.Error())
			continue
		}
		name := head["name"]
		if name == "" {
			name = f.stem
		}
		switch {
		case !profileIDRe.MatchString(name):
			notes = append(notes, fmt.Sprintf("%s: bad name %q", f.label, name))
			continue
		case taken[name]:
			notes = append(notes, fmt.Sprintf("%s: name %q is taken", f.label, name))
			continue
		case body == "":
			notes = append(notes, f.label+": empty body")
			continue
		}
		taken[name] = true
		st := ProfileStep{ID: "main", Title: name, System: body}
		if t := head["tools"]; t != "" && agentToolsReadOnly(t) {
			st.Tools.Preset = agentdag.PresetReadOnly
		}
		switch m := strings.ToLower(head["model"]); {
		case strings.Contains(m, "haiku"), m == "quick", m == "fast":
			st.Category = agentdag.CategoryQuick
		case strings.Contains(m, "opus"), m == "deep":
			st.Category = agentdag.CategoryDeep
		}
		title := head["description"]
		if r := []rune(title); len(r) > SkillDescMaxRunes {
			title = strings.TrimSpace(string(r[:SkillDescMaxRunes-1])) + "…"
		}
		if title == "" {
			title = name
		}
		out = append(out, Profile{Name: name, Title: title, Source: ProfileSourceProject, Steps: []ProfileStep{st}})
	}
	return out, notes
}

// agentToolsReadOnly reports whether a tools list ("Read, Grep") only
// reads.
func agentToolsReadOnly(list string) bool {
	for _, t := range strings.FieldsFunc(list, func(r rune) bool { return r == ',' || r == ' ' || r == '[' || r == ']' }) {
		if !agentReadTools[strings.ToLower(strings.Trim(t, `"'`))] {
			return false
		}
	}
	return true
}

// ChatCommand is one "/name" chat command from a command file.
type ChatCommand struct {
	Name        string
	Description string
	Source      string // SkillSourceProject or SkillSourceUser
	Path        string
	Body        string
}

// ChatCommands is what LoadChatCommands found.
type ChatCommands struct {
	List        []ChatCommand // sorted by name
	Notes       []string
	ProjectHash string // sha256 of the project commands ("" when none)
}

// Find returns the command called name.
func (c ChatCommands) Find(name string) (ChatCommand, bool) {
	for _, x := range c.List {
		if x.Name == name {
			return x, true
		}
	}
	return ChatCommand{}, false
}

// LoadChatCommands reads the user command folders (the user's own; a
// name there wins) and <root>/.claude/commands/*.md.
func LoadChatCommands(root string, userDirs ...string) ChatCommands {
	var c ChatCommands
	seen := map[string]bool{}
	add := func(files []compatFile, source string) {
		for _, f := range files {
			if !ValidSkillName(f.stem) || seen[f.stem] {
				continue
			}
			head, body, err := splitFrontMatter(f.text)
			if err != nil { // no front matter: the whole file is the prompt
				head, body = nil, strings.Trim(f.text, " \t\n")
			}
			if body == "" {
				c.Notes = append(c.Notes, f.label+": empty")
				continue
			}
			seen[f.stem] = true
			c.List = append(c.List, ChatCommand{Name: f.stem, Description: strings.Join(strings.Fields(head["description"]), " "),
				Source: source, Path: f.path, Body: body})
		}
	}
	for _, d := range userDirs {
		if d != "" {
			add(compatDirFiles(d, d, filepath.Base(d), &c.Notes), SkillSourceUser)
		}
	}
	if root != "" {
		add(compatFiles(root, CommandFilesDir, &c.Notes), SkillSourceProject)
	}
	sort.Slice(c.List, func(i, j int) bool { return c.List[i].Name < c.List[j].Name })
	h, n := sha256.New(), 0
	for _, x := range c.List {
		if x.Source == SkillSourceProject {
			fmt.Fprintf(h, "%s\n%s\x00", x.Name, x.Body)
			n++
		}
	}
	if n > 0 {
		c.ProjectHash = hex.EncodeToString(h.Sum(nil))
	}
	return c
}

// Expand is the message the command sends: $ARGUMENTS is replaced by
// args; without it, args (if any) go after a blank line.
func (c ChatCommand) Expand(args string) string {
	if strings.Contains(c.Body, "$ARGUMENTS") {
		return strings.ReplaceAll(c.Body, "$ARGUMENTS", args)
	}
	if args == "" {
		return c.Body
	}
	return c.Body + "\n\n" + args
}

// SplitChatCommand splits "/name rest" into name and rest. ok is false
// when text is not a command (no leading "/", or a path like /a/b).
func SplitChatCommand(text string) (name, args string, ok bool) {
	rest, ok := strings.CutPrefix(text, "/")
	if !ok {
		return "", "", false
	}
	name, args, _ = strings.Cut(rest, " ")
	if !ValidSkillName(name) {
		return "", "", false
	}
	return name, strings.TrimSpace(args), true
}

// compatFile is one *.md file of a compat folder.
type compatFile struct {
	path, label, stem, text string
}

// compatFiles reads <root>/<rel>/*.md (files must stay inside root).
func compatFiles(root, rel string, notes *[]string) []compatFile {
	return compatDirFiles(root, filepath.Join(root, filepath.FromSlash(rel)), rel, notes)
}

func compatDirFiles(base, dir, label string, notes *[]string) []compatFile {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	realBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return nil
	}
	var out []compatFile
	for _, e := range entries {
		file := e.Name()
		if e.IsDir() || strings.HasPrefix(file, ".") || !strings.EqualFold(filepath.Ext(file), ".md") {
			continue
		}
		path := filepath.Join(dir, file)
		text, size, ok := readRulesFile(realBase, path)
		switch {
		case !ok:
			*notes = append(*notes, label+"/"+file+": empty or unreadable")
			continue
		case size > SkillMaxBytes:
			*notes = append(*notes, label+"/"+file+": over 32 KB")
			continue
		}
		out = append(out, compatFile{path: path, label: label + "/" + file, stem: strings.TrimSuffix(file, filepath.Ext(file)), text: text})
	}
	return out
}

// splitFrontMatter returns the "key: value" lines of the front matter
// (keys lower case, quotes taken off) and the trimmed body.
func splitFrontMatter(text string) (map[string]string, string, error) {
	text = strings.TrimLeft(text, "\ufeff \t\n")
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return nil, "", fmt.Errorf("no front matter (--- name / description ---)")
	}
	i := strings.Index("\n"+rest, "\n---")
	if i < 0 {
		return nil, "", fmt.Errorf("no front matter (--- name / description ---)")
	}
	head, body := "", rest
	if i > 0 {
		head, body = rest[:i-1], rest[i-1:]
	}
	body = strings.TrimPrefix(strings.TrimPrefix(body, "\n"), "---")
	m := map[string]string{}
	for _, line := range strings.Split(head, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		m[strings.ToLower(strings.TrimSpace(k))] = v
	}
	return m, strings.Trim(body, " \t\n"), nil
}
