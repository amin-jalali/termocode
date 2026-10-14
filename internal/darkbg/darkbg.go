// Package darkbg stops the 5 s blank screen on terminals that never answer
// an OSC 11 (background colour) query.
//
// Bubble Tea v1 calls lipgloss.HasDarkBackground() in its package init
// (tea_init.go). That asks the terminal with OSC 11 and termenv waits up to
// 5 s for the reply. Some muxers, IDE terminals and plain ptys never reply,
// so termocode showed nothing for 5 s.
//
// termocode does not use the answer: its themes come from the config, and
// main sets the terminal background itself (OSC 11 #121212). So this
// package fixes the value to "dark" before Bubble Tea's init runs.
//
// Init order: Go initialises packages sorted by import path once their
// imports are ready (Go ≥ 1.21). This package only imports lipgloss, and
// "github.com/amin-jalali/…" sorts before "github.com/charmbracelet/bubbletea",
// so its init runs first. cmd/termocode must import it (blank import);
// e2e TestStartupSilentTerminal guards it.
package darkbg

import "github.com/charmbracelet/lipgloss"

func init() { lipgloss.SetHasDarkBackground(true) }
