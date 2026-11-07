package app

import "testing"

// The APK shell has no Termux:API. It sets TERMOCODE_APK_CLIP to its own
// clipboard bridge: "<bridge> set" takes stdin, "<bridge> get" prints
// the clipboard. These tests pin that contract with a fake host.

const fakeBridge = "/apk/bin/termocode-clip"

func apkEnv() map[string]string { return map[string]string{apkClipEnv: fakeBridge} }

func TestClipHostAPKCountsAsAndroid(t *testing.T) {
	h := fakeClipHost("linux", apkEnv(), nil, nil, nil)
	if !h.termux() {
		t.Fatal("termux() = false with an APK bridge, want true (Android rules: no lib, no watch)")
	}
	if h.watch() {
		t.Fatal("watch() = true in the APK, want false (no polling on a phone)")
	}
}

func TestClipToolsAPKBridgeFirst(t *testing.T) {
	h := fakeClipHost("android", apkEnv(), nil, nil, nil)
	tools := h.tools()
	if len(tools) < 2 || tools[0][0] != fakeBridge || tools[0][1] != "set" {
		t.Fatalf("APK tools = %v, want [%s set] first", tools, fakeBridge)
	}
	if tools[1][0] != "termux-clipboard-set" {
		t.Fatalf("APK tools = %v, want termux-clipboard-set after the bridge", tools)
	}
}

func TestClipToolsNoBridgeUnchanged(t *testing.T) {
	h := fakeClipHost("android", nil, nil, nil, nil)
	if tools := h.tools(); len(tools) == 0 || tools[0][0] != "termux-clipboard-set" {
		t.Fatalf("Termux tools = %v, want termux-clipboard-set first", tools)
	}
}

func TestClipAPKWriteUsesBridge(t *testing.T) {
	var runs []fakeRun
	h := fakeClipHost("android", apkEnv(), []string{fakeBridge}, map[string]string{fakeBridge: ""}, &runs)
	if !h.toolWrite("hello") {
		t.Fatal("toolWrite = false, want true through the APK bridge")
	}
	if len(runs) != 1 || runs[0].argv[0] != fakeBridge || runs[0].argv[1] != "set" || runs[0].stdin != "hello" {
		t.Fatalf("runs = %+v, want one [%s set] with stdin hello", runs, fakeBridge)
	}
}

func TestClipAPKReadUsesBridge(t *testing.T) {
	var runs []fakeRun
	h := fakeClipHost("android", apkEnv(), []string{fakeBridge, "termux-clipboard-get"},
		map[string]string{fakeBridge: "from-apk", "termux-clipboard-get": "from-termux"}, &runs)
	got, ok := h.termuxRead()
	if !ok || got != "from-apk" {
		t.Fatalf("termuxRead = %q, %v; want from-apk, true", got, ok)
	}
	if len(runs) != 1 || runs[0].argv[1] != "get" {
		t.Fatalf("runs = %+v, want one [%s get]", runs, fakeBridge)
	}
}

func TestClipAPKReadFallsBackToTermux(t *testing.T) {
	// A broken bridge must not hide a working termux-clipboard-get.
	h := fakeClipHost("android", apkEnv(), []string{fakeBridge, "termux-clipboard-get"},
		map[string]string{"termux-clipboard-get": "from-termux"}, nil)
	if got, ok := h.termuxRead(); !ok || got != "from-termux" {
		t.Fatalf("termuxRead = %q, %v; want from-termux, true", got, ok)
	}
}

func TestClipAPKWriteFailsSoOSC52CanTakeOver(t *testing.T) {
	// Bridge present but failing, no termux-api: toolWrite reports false,
	// so the caller falls back to OSC 52.
	h := fakeClipHost("android", apkEnv(), []string{fakeBridge}, nil, nil)
	if h.toolWrite("x") {
		t.Fatal("toolWrite = true with a failing bridge, want false (OSC 52 fallback)")
	}
}

func TestClipAPKHelp(t *testing.T) {
	h := fakeClipHost("android", apkEnv(), []string{fakeBridge}, nil, nil)
	if got := h.help(); got != apkClipHelp {
		t.Fatalf("help = %q, want apkClipHelp", got)
	}
}

func TestClipAPKBridgeMustBeAbsolute(t *testing.T) {
	// A relative name could be any program on PATH: ignore it.
	h := fakeClipHost("linux", map[string]string{apkClipEnv: "termocode-clip"}, nil, nil, nil)
	if h.apkBridge() != "" || h.termux() {
		t.Fatalf("relative bridge accepted: apkBridge=%q termux=%v", h.apkBridge(), h.termux())
	}
}
