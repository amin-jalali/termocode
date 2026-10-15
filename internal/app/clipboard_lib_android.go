//go:build android

package app

import "errors"

// On Android (Termux) there is no X11 clipboard, and golang.design's
// Android backend needs an APK with a Java VM, so the library is left out
// of the build. The clipboard goes through termux-clipboard-set / -get
// instead (clipboard_termux.go), with OSC 52 as fallback.

func clipLibInit() error { return errors.New("no native clipboard library on Android") }

func clipLibRead() []byte { return nil }

func clipLibWrite(string) bool { return false }
