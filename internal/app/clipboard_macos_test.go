package app

import "testing"

// SCN-35: the clipboard on macOS and Termux with text that is not plain
// ASCII, through the fake clipHost (no real machine, no real process).

func TestSCN35MacOSClipboardUsesPbcopyFirst(t *testing.T) {
	// XQuartz sets DISPLAY on a Mac; pbcopy still comes first and is not Termux.
	h := fakeClipHost("darwin", map[string]string{"DISPLAY": "/private/tmp/launch-x/org.xquartz:0"}, nil, nil, nil)
	tools := h.tools()
	if len(tools) == 0 || tools[0][0] != "pbcopy" {
		t.Fatalf("macOS tools = %v, want pbcopy first", tools)
	}
	if h.termux() {
		t.Fatal("macOS counted as Termux")
	}
	if !h.watch() {
		t.Fatal("macOS clipboard history watcher is off")
	}
}

func TestSCN35MacOSWritePipesUTF8ToPbcopy(t *testing.T) {
	var runs []fakeRun
	h := fakeClipHost("darwin", nil, []string{"pbcopy"}, map[string]string{"pbcopy": ""}, &runs)
	const text = "héllo → 界 🙂\n\ttab"
	if !h.toolWrite(text) {
		t.Fatal("toolWrite failed with pbcopy on PATH")
	}
	if len(runs) != 1 || runs[0].argv[0] != "pbcopy" || runs[0].stdin != text {
		t.Fatalf("runs = %+v, want one pbcopy with the text unchanged", runs)
	}
}

func TestSCN35MacOSWithoutPbcopyFallsBackToOSC52(t *testing.T) {
	h := fakeClipHost("darwin", nil, nil, nil, nil)
	if h.toolWrite("x") {
		t.Fatal("toolWrite reported success with no clipboard command")
	}
	if h.help() != clipboardHelp {
		t.Fatalf("help = %q, want the desktop help", h.help())
	}
}

func TestSCN35TermuxWriteKeepsUTF8(t *testing.T) {
	var runs []fakeRun
	h := fakeClipHost("linux", map[string]string{"TERMUX_VERSION": "0.118.0"}, []string{"termux-clipboard-set"}, map[string]string{"termux-clipboard-set": ""}, &runs)
	const text = "قلب → 界 🙂"
	if !h.toolWrite(text) {
		t.Fatal("toolWrite failed in Termux with termux-api")
	}
	if len(runs) != 1 || runs[0].argv[0] != "termux-clipboard-set" || runs[0].stdin != text {
		t.Fatalf("runs = %+v", runs)
	}
}

func TestSCN35TermuxReadKeepsUTF8(t *testing.T) {
	h := fakeClipHost("android", nil, []string{"termux-clipboard-get"}, map[string]string{"termux-clipboard-get": "界 🙂 é"}, nil)
	if got, ok := h.termuxRead(); !ok || got != "界 🙂 é" {
		t.Fatalf("termuxRead = %q, %v", got, ok)
	}
}
