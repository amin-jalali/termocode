package setup

import (
	"errors"
	"strings"
	"testing"
)

func TestDoctorReport(t *testing.T) {
	oldLook, oldRun := lookPath, runOutput
	defer func() { lookPath, runOutput = oldLook, oldRun }()

	lookPath = func(bin string) (string, error) {
		if bin == "nvim" || bin == "git" {
			return "/usr/bin/" + bin, nil
		}
		return "", errors.New("not found")
	}
	runOutput = func(string, ...string) (string, error) {
		return "NVIM v0.10.1\nBuild type: Release\n", nil
	}
	t.Setenv("COLORTERM", "truecolor")

	checks := Doctor()
	report := Report(checks)

	for _, want := range []string{
		"== core ==",
		"✓ neovim (editor engine) — NVIM v0.10.1 · /usr/bin/nvim",
		"✓ git (Source Control)",
		"? ripgrep (workspace search)",
		"? gopls (Go)",
		"✓ truecolor — COLORTERM=truecolor",
		"All required tools found.",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report missing %q\n---\n%s", want, report)
		}
	}
}

func TestDoctorMissingNvim(t *testing.T) {
	oldLook := lookPath
	defer func() { lookPath = oldLook }()
	lookPath = func(string) (string, error) { return "", errors.New("not found") }

	report := Report(Doctor())
	if !strings.Contains(report, "✗ neovim") || !strings.Contains(report, "1 required tool(s) missing.") {
		t.Errorf("expected nvim to be reported missing:\n%s", report)
	}
}
