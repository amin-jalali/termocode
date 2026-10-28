package clilogin

import "testing"

func TestFirstURL(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"codex stderr", "Starting local login server on http://localhost:1455.\nIf your browser did not open, navigate to this URL to authenticate:\n\nhttps://auth.openai.com/oauth/authorize?response_type=code&client_id=app_x&state=abc\n",
			"https://auth.openai.com/oauth/authorize?response_type=code&client_id=app_x&state=abc"},
		{"claude ink", "Browser didn't open? Use the url below to sign in:\n\nhttps://claude.ai/oauth/authorize?code=true&client_id=c&state=s \n\nPaste code here if prompted > ",
			"https://claude.ai/oauth/authorize?code=true&client_id=c&state=s"},
		{"first of two", "https://a.example/one\nthen https://b.example/two\n", "https://a.example/one"},
		{"trailing dot", "Open https://auth.openai.com/codex/device.\n", "https://auth.openai.com/codex/device"},
		{"in parens", "(see https://x.example/a?b=c)\n", "https://x.example/a?b=c"},
		{"quoted", `url: "https://x.example/q" ok` + "\n", "https://x.example/q"},
		{"http only", "callback on http://localhost:1455/auth\n", ""},
		{"not finished yet", "go to https://auth.openai.com/oauth/auth", ""},
		{"none", "Starting…\n", ""},
	}
	for _, c := range cases {
		if got := firstURL(c.in); got != c.want {
			t.Errorf("%s: firstURL = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestCleanText(t *testing.T) {
	in := "\x1b[2K\x1b[1G\x1b[1mSign in\x1b[22m\r\nline\rnext\x1b]8;;https://hidden.example\x1b\\link\x1b]8;;\x1b\\\n"
	if got, want := cleanText(in), "Sign in\nline\nnextlink\n"; got != want {
		t.Fatalf("cleanText = %q, want %q", got, want)
	}
}

func TestNeedsCode(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"https://claude.ai/oauth/authorize?x=1\n\nPaste code here if prompted > ", true},
		{"Enter the authorization code:", true},
		{"Code? ", true},
		{"https://claude.ai/oauth/authorize?code=true\n", false},
		{"Opening browser…\n", false},
		{"Paste code here if prompted >\nLogin successful.\n", false},
	}
	for _, c := range cases {
		if got := needsCode(c.in); got != c.want {
			t.Errorf("needsCode(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestDeviceCode(t *testing.T) {
	out := "1. Open this link in your browser and sign in\n   https://auth.openai.com/codex/device\n2. Enter this one-time code (expires in 15 minutes)\n   ABCD-12345\n"
	if got := deviceCode(out); got != "ABCD-12345" {
		t.Fatalf("deviceCode = %q", got)
	}
	if got := deviceCode("https://x.example/a\nno code here\n"); got != "" {
		t.Fatalf("deviceCode = %q", got)
	}
	// A code-like word before the link is not the code.
	if got := deviceCode("UTF-8 mode\nhttps://x.example/a\n"); got != "" {
		t.Fatalf("deviceCode = %q", got)
	}
}

func TestNodeMissingText(t *testing.T) {
	for _, s := range []string{
		"/usr/bin/env: 'node': No such file or directory",
		"env: node: No such file or directory",
		"sh: 1: node: not found",
		"exec: node: not found",
	} {
		if !nodeMissing(s) {
			t.Errorf("nodeMissing(%q) = false", s)
		}
	}
	if nodeMissing("Login successful") {
		t.Error("plain text is not node missing")
	}
}
