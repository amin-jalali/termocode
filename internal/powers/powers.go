// Package powers is termocode's catalog of powers (CMP-78): a power is a
// skill (knowledge the AI loads only when a task needs it) and, for some,
// a service to connect with AI: Connect a Service (its MCP tools stay
// deferred until a search needs them). Installing a power writes its
// skill into the user's skills folder as <name>/SKILL.md; removing it
// deletes that file only while it is unchanged. Nothing is fetched from
// the network: the catalog ships with termocode.
package powers

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Power is one catalog entry.
type Power struct {
	ID          string
	Title       string
	Description string // one line: when the AI should load the skill
	MCP         string // the Connect a Service preset ("" none)
	Body        string // the skill's text
}

// SkillName is the name of the power's skill.
func (p Power) SkillName() string { return "power-" + p.ID }

// SkillText is the power's SKILL.md.
func (p Power) SkillText() string {
	return "---\nname: " + p.SkillName() + "\ndescription: " + p.Description + "\n---\n\n# " + p.Title + "\n\n" + p.Body
}

// ErrChanged: the installed skill file is not the catalog's text any more.
var ErrChanged = errors.New("the skill file was changed")

func skillPath(dir string, p Power) string { return filepath.Join(dir, p.SkillName(), "SKILL.md") }

// Installed reports whether dir holds the power's skill file.
func Installed(dir string, p Power) bool {
	_, err := os.Stat(skillPath(dir, p))
	return err == nil
}

// Install writes the power's skill into dir; a file that is there stays
// (os.ErrExist).
func Install(dir string, p Power) error {
	if dir == "" {
		return errors.New("no skills folder")
	}
	path := skillPath(dir, p)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, err = f.WriteString(p.SkillText())
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// Remove deletes the power's skill file and its folder when the folder is
// then empty. An edited file stays (ErrChanged).
func Remove(dir string, p Power) error {
	path := skillPath(dir, p)
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if string(b) != p.SkillText() {
		return ErrChanged
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	_ = os.Remove(filepath.Dir(path)) // only when empty
	return nil
}

// ByID is the catalog power id.
func ByID(id string) (Power, bool) {
	for _, p := range Catalog() {
		if p.ID == id {
			return p, true
		}
	}
	return Power{}, false
}

// Catalog lists the powers in the order the picker shows them.
func Catalog() []Power {
	return []Power{
		{ID: "github-pr-review", Title: "GitHub pull request review", MCP: "github",
			Description: "Review a GitHub pull request: read the diff and checks, then leave clear review notes",
			Body: "Use this when the user asks to review, check or comment on a GitHub pull request.\n\n" +
				"1. Find the pull request (number, branch or link). Read its title, description and linked issue.\n" +
				"2. Read the changed files and the CI checks. A failed check comes first in your notes.\n" +
				"3. Look for bugs first: wrong logic, missing error handling, races, unsafe input, missing tests.\n" +
				"4. Then smaller points: names, dead code, docs. Keep each note short and say why.\n" +
				"5. Show the notes to the user. Post a review or a comment only when the user says so.\n\n" +
				"The GitHub tools come from the GitHub service (AI: Connect a Service). Without it, use `gh pr view` and `gh pr diff` in the terminal.\n"},
		{ID: "gitlab-mr", Title: "GitLab merge requests", MCP: "gitlab",
			Description: "Work with GitLab merge requests and pipelines: list, read, review and explain failed jobs",
			Body: "Use this when the user asks about GitLab merge requests, pipelines or jobs.\n\n" +
				"- To review: read the merge request's changes and discussions, then list bugs first, small points after.\n" +
				"- A failed pipeline: find the first failed job, read the end of its log, and say the cause in one line before any fix.\n" +
				"- Never merge, close or approve without the user's clear yes.\n\n" +
				"The tools come from the GitLab service (AI: Connect a Service). Self-managed GitLab: the user changes the url there.\n"},
		{ID: "sentry-triage", Title: "Sentry error triage", MCP: "sentry",
			Description: "Triage Sentry errors: find the newest unresolved issues, read the stack trace and find the code",
			Body: "Use this when the user asks about production errors, crashes or Sentry issues.\n\n" +
				"1. List the newest unresolved issues; sort by how many users they hit.\n" +
				"2. For one issue, read the stack trace and the last breadcrumbs. Find the top frame that is in this repository.\n" +
				"3. Open that file and line. Say the likely cause in one or two lines.\n" +
				"4. Propose a fix with a test that fails before it. Resolve the issue in Sentry only when the user says so.\n\n" +
				"The tools come from the Sentry service (AI: Connect a Service).\n"},
		{ID: "linear-issues", Title: "Linear issues", MCP: "linear",
			Description: "Plan work from Linear issues: read assigned issues, split them into steps and keep their state current",
			Body: "Use this when the user talks about Linear issues, tickets or the next task.\n\n" +
				"- Read the issue, its comments and its parent. Restate the goal in one line.\n" +
				"- Split the work into small steps with a test each; name the branch after the issue id.\n" +
				"- Change an issue's state or write a comment only when the user says so.\n\n" +
				"The tools come from the Linear service (AI: Connect a Service).\n"},
		{ID: "release-notes", Title: "Release notes from git",
			Description: "Write release notes from the git history between two tags: group changes, plain words, no internals",
			Body: "Use this when the user asks for release notes, a changelog or what changed since a version.\n\n" +
				"1. Find the two points (the last tag and HEAD, unless the user names others). Read `git log --oneline A..B`.\n" +
				"2. Group the changes: New, Better, Fixed. Drop test-only, CI-only and refactor-only commits.\n" +
				"3. Write one short line per change in the user's words, not the commit's. Name the feature, not the file.\n" +
				"4. Show the notes; write them into a file only when the user says so.\n"},
	}
}
