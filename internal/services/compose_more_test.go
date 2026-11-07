package services_test

import (
	"context"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/services"
)

// CTX-8: pull, build, up with options and scale are visible jobs with
// the exact argv the card / tab shows.
func TestComposePullBuildUpOptionsScale(t *testing.T) {
	c, _ := newCompose(t)
	pre := "compose -f compose.yaml --profile dev "
	for _, tc := range []struct {
		name  string
		cmd   services.Command
		label string
		argv  string
	}{
		{"pull one", c.Pull([]string{"api"}), "services: pull", "pull api"},
		{"pull all", c.Pull(nil), "services: pull", "pull"},
		{"build one", c.Build([]string{"api"}), "services: build", "build api"},
		{"build all", c.Build(nil), "services: build", "build"},
		{"up build", c.UpWith([]string{"api"}, services.UpOptions{Build: true}), "services: up", "up -d --build api"},
		{"up all opts", c.UpWith([]string{"api"}, services.UpOptions{Build: true, ForceRecreate: true, NoDeps: true}), "services: up",
			"up -d --build --force-recreate --no-deps api"},
		{"up pull always", c.UpWith(nil, services.UpOptions{PullAlways: true}), "services: up", "up -d --pull always"},
		{"up plain", c.UpWith(nil, services.UpOptions{}), "services: up", "up -d"},
		{"scale", c.Scale("api", 3), "services: up", "up -d --no-recreate --scale api=3 api"},
		{"scale zero", c.Scale("api", 0), "services: up", "up -d --no-recreate --scale api=0 api"},
	} {
		if tc.cmd.Label != tc.label || strings.Join(tc.cmd.Argv[1:], " ") != pre+tc.argv || tc.cmd.Dir != c.Root {
			t.Errorf("%s = %+v, want %q", tc.name, tc.cmd, pre+tc.argv)
		}
	}
}

// Down with volumes is a captured `down --volumes`; plain Down keeps them.
func TestComposeDownVolumes(t *testing.T) {
	c, f := newCompose(t)
	ctx := context.Background()
	if err := c.Down(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.DownVolumes(ctx); err != nil {
		t.Fatal(err)
	}
	want := "compose -f compose.yaml --profile dev down\ncompose -f compose.yaml --profile dev down --volumes"
	if got := strings.Join(f.Calls(), "\n"); got != want {
		t.Errorf("calls =\n%s\nwant\n%s", got, want)
	}
}

// The config lists the project's named volumes (sorted) for the card.
func TestParseConfigVolumes(t *testing.T) {
	cfg, err := services.ParseConfig([]byte(`{"name":"p","services":{"db":{"image":"postgres"}},
		"volumes":{"pgdata":{"name":"p_pgdata"},"cache":{}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cfg.Volumes, ",") != "cache,pgdata" {
		t.Errorf("Volumes = %q", cfg.Volumes)
	}
	none, _ := services.ParseConfig([]byte(`{"name":"p","services":{}}`))
	if len(none.Volumes) != 0 {
		t.Errorf("no volumes = %q", none.Volumes)
	}
}
