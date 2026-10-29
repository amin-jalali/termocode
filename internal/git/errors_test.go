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
		"fatal: unable to access 'http://127.0.0.1:1/r.git/': Failed to connect to 127.0.0.1 port 1: Connection refused": "unable to access 'http://127.0.0.1:1/r.git/': Failed to connect to 127.0.0.1 port 1: Connection refused",
	} {
		if got := errorReason(out); got != want {
			t.Errorf("errorReason(%q)\n got %q\nwant %q", out, got, want)
		}
	}
}
