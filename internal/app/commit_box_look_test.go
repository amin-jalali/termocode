package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/theme"
)

// Spec (UI review #14): Commit always looks like the panel's main button:
// an accent-tinted button with accent text while there is nothing to
// commit yet, the solid accent fill once there is.
func TestCommitButtonLooksLikeAButton(t *testing.T) {
	refreshCommitBoxColors()
	m := Model{}
	idle := m.gitActionButtons(40)[0]
	if idle.bg != theme.Wash(theme.BgSidebar, theme.WashSelected) || idle.fg != theme.LG(theme.BorderFocus) {
		t.Errorf("idle Commit: bg %v fg %v, want the accent tint and accent text", idle.bg, idle.fg)
	}
	m.gitCommitMsg = "fix"
	ready := m.gitActionButtons(40)[0]
	if ready.bg != theme.LG(theme.BorderFocus) {
		t.Errorf("ready Commit: bg %v, want the solid accent", ready.bg)
	}
}

// Spec (UI review #14): the Source Control title names the project, like
// the Explorer's ("SOURCE CONTROL · TERMOCODE").
func TestGitTitleNamesProject(t *testing.T) {
	got := ansi.Strip(renderGitTitleRow(60, false))
	if p := projectName(); p != "" && !strings.Contains(got, "SOURCE CONTROL · "+p) {
		t.Errorf("title %q, want the project %q", got, p)
	}
}
