package main

import tea "github.com/charmbracelet/bubbletea"

// pacingWindow is how many frame bytes may wait in the network before the
// next frame waits for the terminal (Group S2). Over a 1 Mbit/s line it
// is about 0.25 s of output: the run-on after a held key is let go.
const pacingWindow = 32 << 10

// framePacing reports whether frames are paced to the terminal's answers
// (tea.WithFramePacing): over SSH, where the terminal may be far away
// behind a slow line. TERMOCODE_FRAME_PACING=on / off overrides it.
func framePacing(getenv func(string) string) bool {
	switch getenv("TERMOCODE_FRAME_PACING") {
	case "on", "1":
		return true
	case "off", "0":
		return false
	}
	return getenv("SSH_CONNECTION") != "" || getenv("SSH_CLIENT") != "" || getenv("SSH_TTY") != ""
}

func framePacingOption(on bool) tea.ProgramOption {
	if !on {
		return func(*tea.Program) {}
	}
	return tea.WithFramePacing(pacingWindow)
}
