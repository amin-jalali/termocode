package git

import "testing"

// Spec (SCN-13): over ssh, git ends every failure with the same "Could not
// read from remote repository." line; the toast must show ssh's line
// before it, which says why (no network, refused, no key).
func TestErrorReasonSSH(t *testing.T) {
	const tail = "fatal: Could not read from remote repository.\n\nPlease make sure you have the correct access rights\nand the repository exists."
	for out, want := range map[string]string{
		"ssh: connect to host 127.0.0.1 port 2222: Connection refused\r\n" + tail:                        "ssh: connect to host 127.0.0.1 port 2222: Connection refused",
		"ssh: Could not resolve hostname example.invalid: Name or service not known\n" + tail:            "ssh: Could not resolve hostname example.invalid: Name or service not known",
		"Connection timed out during banner exchange\nConnection to 10.0.0.1 port 22 timed out\n" + tail: "Connection timed out during banner exchange",
		"git@example.com: Permission denied (publickey).\n" + tail:                                       "git@example.com: Permission denied (publickey).",
		// Nothing before it: the fatal line is all there is.
		tail: "Could not read from remote repository.",
		// Other fatal lines are kept as they are.
		"fatal: repository 'x' does not exist": "repository 'x' does not exist",
	} {
		if got := errorReason(out); got != want {
			t.Errorf("errorReason(%q)\n got %q\nwant %q", out, got, want)
		}
	}
}

// Spec (SCN-13): over http git says "unable to access '<url>': <cause>";
// the toast's one line is short, so the cause comes first and the URL
// (the user's own remote) is dropped.
func TestErrorReasonHTTP(t *testing.T) {
	for out, want := range map[string]string{
		"fatal: unable to access 'http://127.0.0.1:1/r.git/': Failed to connect to 127.0.0.1 port 1 after 0 ms: Couldn't connect to server": "Failed to connect to 127.0.0.1 port 1 after 0 ms: Couldn't connect to server",
		"fatal: unable to access 'https://example.test/r.git/': Operation too slow. Less than 1 bytes/sec transferred the last 60 seconds":  "Operation too slow. Less than 1 bytes/sec transferred the last 60 seconds",
		"fatal: unable to access 'https://example.test/r.git/': Could not resolve host: example.test":                                       "Could not resolve host: example.test",
	} {
		if got := errorReason(out); got != want {
			t.Errorf("errorReason(%q)\n got %q\nwant %q", out, got, want)
		}
	}
}
