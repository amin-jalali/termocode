// Package testkit drives the real termocode app model in tests, so UI
// behaviour is checked by machines instead of by hand.
//
// A Driver owns one app model at a fixed size in a private sandbox:
//
//	d := testkit.New(t, testkit.Options{W: 120, H: 40, Git: true})
//	d.Palette("toggle-terminal")    // run a command-palette entry by id
//	d.Key("ctrl+p")                 // a key, named as tea.KeyMsg.String()
//	d.Type("main")                  // typed runes
//	d.Move(10, 5); d.Click(10, 5)   // mouse motion / left click
//	d.Wheel(10, 5, true)            // wheel up (false = down)
//	d.Drag(30, 10, 50, 10)          // press, move, release
//	d.Resize(80, 24)
//	d.Action("view:git")            // any app.DevActionMsg id
//	frame := d.Frame()              // the last View()
//
// Every step calls Update, runs the returned tea.Cmds in goroutines, feeds
// their messages back into Update until things go quiet (Options.Quiet,
// capped by Options.MaxSettle), then renders View and checks it with
// CheckFrame. Failures are reported with t.Errorf and the last steps, so a
// test can keep going; Driver.Failed tells whether the test failed. An
// Update or View that does not return within Options.StepTimeout fails the
// test at once (t.Fatalf) with a goroutine dump: that is a hang.
//
// The sandbox: a temp workspace (the process cwd while the driver lives),
// optionally a git repo with a few commits, and private HOME, XDG_* and
// plugin dirs, so a test never reads or writes the user's real
// ~/.config/termocode, never touches the system clipboard (DISPLAY and
// WAYLAND_DISPLAY are cleared) and never reaches the network (AI keys are
// cleared and HTTP(S) proxies point at a closed port). Without Options.Nvim
// the app runs with no embedded nvim (TERMOCODE_NO_NVIM=1); with it the
// test is skipped when nvim is not on PATH.
//
// Because the driver changes the cwd and environment, tests that use it
// must not call t.Parallel.
//
// CheckFrame (and FrameProblems, which returns the problems instead of
// failing) asserts the frame invariants: valid UTF-8, exactly h rows,
// every row exactly w cells wide (charmbracelet/x/ansi StringWidth), and no
// control bytes other than SGR (ESC [ … m), OSC 8 hyperlinks and image
// data (kitty APC "ESC _ G…", sixel DCS), which the images setting emits.
package testkit
