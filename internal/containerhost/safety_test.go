package containerhost

import (
	"os"
	"testing"
)

// hostileNames are file names that would run code or act as options if
// a path were ever pasted into a script.
var hostileNames = []string{
	"a b.txt",
	"it's.txt",
	`dq"x.txt`,
	"$(mkdir CANARY).txt",
	"`mkdir CANARY`.txt",
	"x; mkdir CANARY",
	"-rf",
	"--help",
	"line\nbreak.txt",
	"star*.txt",
	`back\slash`,
	"$HOME",
	" lead space",
}

func TestHostileNamesStayData(t *testing.T) {
	h, _, c := newHost(t)
	f, ctx := h.FS(), t.Context()
	for _, name := range hostileNames {
		if err := f.WriteFile(ctx, name, []byte(name), 0o644); err != nil {
			t.Fatalf("WriteFile(%q): %v", name, err)
		}
		if data, err := f.ReadFile(ctx, name); err != nil || string(data) != name {
			t.Fatalf("ReadFile(%q) = %q, %v", name, data, err)
		}
		if fi, err := f.Stat(ctx, name); err != nil || fi.Name() != name {
			t.Fatalf("Stat(%q) = %v, %v", name, fi, err)
		}
		dir := "dir " + name
		if err := f.Mkdir(ctx, dir, 0o755); err != nil {
			t.Fatalf("Mkdir(%q): %v", dir, err)
		}
		if err := f.Rename(ctx, name, dir+"/"+name); err != nil {
			t.Fatalf("Rename(%q): %v", name, err)
		}
		ents, err := f.ReadDir(ctx, dir)
		if err != nil || len(ents) != 1 || ents[0].Name() != name {
			t.Fatalf("ReadDir(%q) = %v, %v", dir, ents, err)
		}
		if err := f.Remove(ctx, dir); err != nil {
			t.Fatalf("Remove(%q): %v", dir, err)
		}
	}
	ents, err := f.ReadDir(ctx, ".")
	if err != nil || len(ents) != 0 {
		t.Fatalf("left over: %v, %v", names(ents), err)
	}
	for _, p := range []string{"/CANARY", "/app/CANARY"} {
		if _, err := os.Stat(c.Local(p)); !os.IsNotExist(err) {
			t.Fatalf("a name ran as code: %s exists", p)
		}
	}
}

func TestHostileExecArgsAreNotShellParsed(t *testing.T) {
	h, _, c := newHost(t)
	for _, name := range hostileNames {
		if _, _, code, err := h.Exec().Capture(t.Context(), "", "echo", name); err != nil || code != 0 {
			t.Fatalf("echo %q: %d %v", name, code, err)
		}
	}
	if _, err := os.Stat(c.Local("/app/CANARY")); !os.IsNotExist(err) {
		t.Fatal("an argument ran as code")
	}
}
