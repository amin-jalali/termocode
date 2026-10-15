//go:build !android

package app

import "golang.design/x/clipboard"

// The golang.design/x/clipboard calls live here so Android builds can
// leave the library out: on Android it needs cgo, golang.org/x/mobile and
// a Java VM, none of which a Termux binary has (see clipboard_lib_android.go).

func clipLibInit() error { return clipboard.Init() }

func clipLibRead() []byte { return clipboard.Read(clipboard.FmtText) }

// clipLibWrite reports whether the library took the clipboard (it returns
// a nil channel when it could not).
func clipLibWrite(s string) bool {
	return clipboard.Write(clipboard.FmtText, []byte(s)) != nil
}
