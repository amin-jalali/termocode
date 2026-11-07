package services

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestPlanGitDeployClonesThenUps(t *testing.T) {
	base := t.TempDir()
	d, err := PlanGitDeploy("https://github.com/acme/shop.git#v1.2", base)
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "shop" || d.Ref != "v1.2" || d.Dir != filepath.Join(base, "shop") || d.Exists {
		t.Fatalf("plan %+v", d)
	}
	steps := d.Steps("docker")
	want := [][]string{
		{"git", "clone", "--depth", "1", "--branch", "v1.2", "--", "https://github.com/acme/shop.git", d.Dir},
		{"docker", "compose", "--project-directory", d.Dir, "up", "-d"},
	}
	if len(steps) != 2 || !slices.Equal(steps[0], want[0]) || !slices.Equal(steps[1], want[1]) {
		t.Fatalf("steps %q", steps)
	}
}

func TestPlanGitDeployPullsAnExistingCheckout(t *testing.T) {
	base := t.TempDir()
	d, err := PlanGitDeploy("git@github.com:acme/shop", base)
	if err != nil {
		t.Fatal(err)
	}
	d.Exists = true
	steps := d.Steps("")
	if !slices.Equal(steps[0], []string{"git", "-C", d.Dir, "pull", "--ff-only"}) || steps[1][0] != "docker" {
		t.Fatalf("steps %q", steps)
	}
}

func TestPlanGitDeployRefusesBadInput(t *testing.T) {
	for _, bad := range []string{"", "-uhttps://x", "ftp://x/y.git", "https://x/y.git#-x", "https://x/y.git#a..b",
		"https://x/ y.git", "https://x/", "file:///etc/repo", "https://x/y.git#main\n"} {
		if _, err := PlanGitDeploy(bad, t.TempDir()); err == nil {
			t.Fatalf("%q: want an error", bad)
		}
	}
}
