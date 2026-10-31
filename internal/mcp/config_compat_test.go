package mcp

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Pasted configs from other apps use ${env:NAME}, ${workspaceFolderBasename}
// and ${/}; they resolve like termocode's own ${NAME} and ${workspaceFolder}.
func TestExpandOtherAppsVariables(t *testing.T) {
	ws := filepath.Join(t.TempDir(), "my-proj")
	env := map[string]string{"TOKEN": "abc"}
	x := Expander{
		Env:       func(k string) (string, bool) { v, ok := env[k]; return v, ok },
		Workspace: ws,
		Home:      "/home/u",
	}
	s := Server{Name: "a", Command: "run", Args: []string{
		"${env:TOKEN}", "${workspaceFolderBasename}", "a${/}b", "${env:MISSING:-dflt}",
	}}
	got, err := s.Expand(x)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"abc", "my-proj", "a" + string(os.PathSeparator) + "b", "dflt"}
	for i := range want {
		if got.Args[i] != want[i] {
			t.Errorf("arg %d = %q, want %q", i, got.Args[i], want[i])
		}
	}
	_, err = Server{Name: "b", Command: "${env:NOPE}"}.Expand(x)
	var ne *NeedsError
	if !errors.As(err, &ne) || ne.What != "${NOPE}" {
		t.Errorf("missing ${env:NOPE}: err = %v, want needs ${NOPE}", err)
	}
}
