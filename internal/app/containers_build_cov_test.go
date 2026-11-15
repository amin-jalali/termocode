package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Build: the steps check KEY=value, no engine says so, the card runs a
// tab; the palette finds the workspace Dockerfile or Containerfile.
func TestCovContainerBuildSteps(t *testing.T) {
	m, _ := ctrModel(t)
	root := m.services.root
	t.Chdir(root)
	m.promptOpen = false
	_ = m.buildDockerfileFromPalette()
	if m.promptOpen || !strings.Contains(ctrToast(m), "No Dockerfile here") {
		t.Fatalf("palette without a Dockerfile: %s", ctrToast(m))
	}
	if err := os.WriteFile(filepath.Join(root, "Containerfile"), []byte("FROM alpine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = m.buildDockerfileFromPalette()
	if !m.promptOpen || m.pendingBuild.Dockerfile != filepath.Join(root, "Containerfile") {
		t.Fatalf("palette: %+v", m.pendingBuild)
	}
	if err := os.WriteFile(filepath.Join(root, "Dockerfile"), []byte("FROM alpine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = m.askImageBuild(filepath.Join(root, "Dockerfile"))
	_ = m.onContainerBuildPrompt(promptKindContainerBuildTag, "web:1")
	_ = m.onContainerBuildPrompt(promptKindContainerBuildArgs, "=x")
	if !strings.Contains(ctrToast(m), "Build arguments must use KEY=value") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = m.askImageBuild(filepath.Join(root, "Dockerfile"))
	_ = m.onContainerBuildPrompt(promptKindContainerBuildTag, "web:1")
	_ = m.onContainerBuildPrompt(promptKindContainerBuildArgs, "")
	_ = m.onContainerBuildPrompt(promptKindContainerBuildTarget, "")
	_ = m.onContainerBuildPrompt(promptKindContainerBuildPlatform, "")
	if !m.confirmOpen || m.pendingHostOperation != "build-image" {
		t.Fatal("no build card")
	}
	_ = m.runHostAction("build")
	if !strings.Contains(ctrToast(m), "Cannot build image") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	m.services.containerEngine.Binary = ""
	_ = m.askBuildConfirm()
	if !strings.Contains(ctrToast(m), "No container engine found") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	m.services.containerEngine.Binary = "docker"
	if defaultBuildTag("/w/My App!/Dockerfile") != "my-app:latest" || defaultBuildTag("/w/___/Dockerfile") != "image:latest" {
		t.Fatal(defaultBuildTag("/w/My App!/Dockerfile"))
	}
	ctrNvim(t, m)
	m.pendingBuild.Dockerfile = filepath.Join(root, "Dockerfile")
	if m.runImageBuild() != nil {
		t.Fatalf("build tab: %s", ctrToast(m))
	}
}
