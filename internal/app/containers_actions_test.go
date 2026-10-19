package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/containers"
)

func TestContainerTerminalCommandsAndComposeArguments(t *testing.T) {
	e := containers.Engine{Binary: "docker"}
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"logs", svcShellLine(e.LogsFollow("abc").Argv), "docker logs -f --tail 200 abc"},
		{"shell", svcShellLine(e.Shell("abc").Argv), `docker exec -it abc sh -c 'command -v bash >/dev/null && exec bash || exec sh'`},
		{"attach", svcShellLine(e.Attach("abc").Argv), "docker attach --sig-proxy=false abc"},
		{"stats", svcShellLine(e.StatsFollow("abc").Argv), "docker stats abc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Fatalf("command = %q, want %q", tc.got, tc.want)
			}
		})
	}
	cmd := e.Compose("shop", "/work/shop", "down")
	got := svcShellLine(cmd.Argv)
	if want := "docker compose -p shop --project-directory /work/shop down"; got != want {
		t.Fatalf("compose command = %q, want %q", got, want)
	}
}

func TestContainerInspectPageIsReadOnlyJSON(t *testing.T) {
	encoded, err := json.Marshal(map[string]any{"Id": "abc", "State": map[string]any{"Status": "running"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"Status":"running"`) {
		t.Fatalf("inspect fixture was not encoded as JSON: %s", encoded)
	}
	page := openReadOnlyPageLua
	if !strings.Contains(page, "modifiable = false") || !strings.Contains(page, "readonly = true") || !strings.Contains(page, "filetype = ft") {
		t.Fatalf("page script does not create a read-only JSON buffer: %s", page)
	}
}
