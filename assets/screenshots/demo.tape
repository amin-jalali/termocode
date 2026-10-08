# demo.tape — records assets/screenshots/demo.gif plus three still shots.
#
# Needs: vhs (https://github.com/charmbracelet/vhs), nvim, rg, git,
# and the JetBrainsMono Nerd Font installed on the machine.
#
# Run from the repo root:
#   go build -o termocode ./cmd/termocode
#   vhs assets/screenshots/demo.tape
#
# The tape opens termocode on this repo itself, so the shots show real Go code.
# vhs cannot send function keys, so the F1 palette and Source Control shots are
# taken by hand — see README.md in this folder. Raise the Sleep values if your
# machine is slow (the first launch also clones a few nvim plugins).

Output assets/screenshots/demo.gif

Require nvim
Require git

Set Shell "bash"
Set FontFamily "JetBrainsMono Nerd Font"
Set FontSize 15
Set Width 1600
Set Height 960
Set Padding 0
Set TypingSpeed 60ms

# Launch. TERMOCODE_FONT_DELTA=0 skips the OSC font-size request.
Hide
Type "TERMOCODE_FONT_DELTA=0 ./termocode ." Enter
Sleep 4s
Show

# 1. Quick Open (Ctrl+P) a file → editor.png
Ctrl+P
Sleep 600ms
Type "model.go"
Sleep 800ms
Enter
Sleep 1.5s
Down@80ms 12
Sleep 500ms
Screenshot assets/screenshots/editor.png

# 2. Workspace search (Alt+F) → search.png
Alt+F
Sleep 500ms
Type "ExecLua"
Sleep 1.5s
Down@200ms 3
Sleep 500ms
Screenshot assets/screenshots/search.png
Escape
Sleep 500ms

# 3. Integrated terminal (Ctrl+T) → terminal.png
Ctrl+T
Sleep 1.5s
Type "git log --oneline | head -5" Enter
Sleep 1s
Screenshot assets/screenshots/terminal.png
Ctrl+T
Sleep 600ms

# Quit
Ctrl+Q
Sleep 1s
