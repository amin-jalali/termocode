package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// registry_manage: Connect Registry… opens the GUI's own sign-in prompt
// (the user types the user and token there; the model never sees them),
// Sign Out is the GUI's card (every time, no "Always allow"), the catalog
// reads at once and deleting a remote tag is the GUI's card.

func TestAIRegistryManage(t *testing.T) {
	m, f, call := aiContainersTestModel(t)
	cfg := os.Getenv("DOCKER_CONFIG")
	if err := os.WriteFile(filepath.Join(cfg, "config.json"), []byte(`{"auths":{"ghcr.io":{"auth":"eDp5"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	tl := aiContainersTool(t, m.aiContainersTools(call), "registry_manage")
	if got := aiParamEnum(tl.Params["op"]); strings.Join(got, ",") != "connect,signout,catalog,delete_tag" {
		t.Errorf("ops = %v", got)
	}
	for op, w := range map[string]bool{"connect": false, "catalog": false, "signout": true, "delete_tag": true} {
		ask, class := tl.Gate(map[string]any{"op": op})
		if ask != w || class != "" {
			t.Errorf("%s gate = %v %q", op, ask, class)
		}
	}
	out, err := tl.Run(context.Background(), map[string]any{"op": "connect", "host": "ghcr.io"})
	if err != nil {
		t.Fatal(err)
	}
	covAIMustContain(t, "connect", out, "the IDE asks the user", "never")
	if m.services.regPending.host != "ghcr.io" || !m.promptOpen {
		t.Errorf("the sign-in prompt did not open: %+v %v", m.services.regPending, m.promptOpen)
	}
	a := map[string]any{"op": "signout", "host": "ghcr.io"}
	if err := tl.Validate(a); err != nil {
		t.Fatal(err)
	}
	covAIMustContain(t, "signout card", tl.Describe(a), "Sign out of ghcr.io", "docker logout ghcr.io", "Local images stay")
	if _, err := tl.Run(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if f.Count("logout ghcr.io") != 1 {
		t.Errorf("calls %q", f.Calls())
	}
	for _, bad := range []map[string]any{
		{"op": "signout", "host": "docker.io"}, // not signed in
		{"op": "signout", "host": "-x"},
		{"op": "delete_tag", "ref": "ghcr.io/acme/api"}, // no tag
		{"op": "catalog", "host": "a b"},
	} {
		if err := tl.Validate(bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}
