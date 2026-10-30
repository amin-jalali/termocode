//go:build unix

package webgw

import (
	"errors"
	"fmt"
	"html"
	"os"
	"syscall"
)

// The opener (old plan 3.1): the one-time link must never be a program's
// argument (every user can read argv). The UI writes it into a 0600 page
// that sends the browser on, opens the page's path, and removes it once
// the link has expired.

// LaunchURL is the one-time link: the secret sits in the #fragment, which
// the browser never sends to a server or puts in a Referer.
func LaunchURL(port int, secret string) string {
	return fmt.Sprintf("http://127.0.0.1:%d/#s=%s", port, secret)
}

// WriteRedirect writes the redirect page for url at path, mode 0600. An
// old file is replaced; a symlink at path is replaced, never followed.
func WriteRedirect(path, url string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	u := html.EscapeString(url)
	page := "<!doctype html><meta charset=\"utf-8\"><meta name=\"referrer\" content=\"no-referrer\">" +
		"<meta http-equiv=\"refresh\" content=\"0;url=" + u + "\"><title>termocode</title>" +
		"<p>Opening termocode…</p>\n"
	if _, err := f.WriteString(page); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return err
	}
	return f.Close()
}
