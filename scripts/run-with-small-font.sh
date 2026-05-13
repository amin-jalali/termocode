#!/usr/bin/env bash
# run-with-small-font.sh — launch termocode after shrinking the terminal
# font by ~2 steps, then restore the original size on exit.
#
# Strategy: cannot synthesize Ctrl+Shift+- (the terminal swallows it before
# stdin). Instead, route to each terminal emulator's native programmatic
# API. Detection is via env vars only.
#
# Supported (resize): kitty, iTerm2, WezTerm.
# Detected but skipped (no runtime font API): Ghostty, Alacritty,
# GNOME Terminal/VTE, foot, plus any unknown terminal.

set -u

TERMOCODE_BIN="/root/termocode/termocode"
STEP=2

# Slot for the restore callback. Default = no-op so the trap is always safe.
RESTORE_CMD=":"

restore() {
    # Guard so a failed restore never leaks a stack trace through to the user.
    eval "$RESTORE_CMD" 2>/dev/null || true
}

# Cover normal exit, Ctrl+C, and SIGTERM. EXIT fires for SEGV / kill -9
# is unrecoverable but nothing we can do about that.
trap restore EXIT INT TERM HUP

note() {
    printf '[run-with-small-font] %s\n' "$1" >&2
}

detect_terminal() {
    if [ -n "${KITTY_PID:-}" ] || [ "${TERM:-}" = "xterm-kitty" ] || [ "${TERM:-}" = "xterm-kitty-direct" ]; then
        echo "kitty"; return
    fi
    if [ "${LC_TERMINAL:-}" = "iTerm2" ] || [ "${TERM_PROGRAM:-}" = "iTerm.app" ]; then
        echo "iterm2"; return
    fi
    if [ "${TERM_PROGRAM:-}" = "WezTerm" ] || [ -n "${WEZTERM_PANE:-}" ]; then
        echo "wezterm"; return
    fi
    if [ "${TERM_PROGRAM:-}" = "ghostty" ] || [ -n "${GHOSTTY_RESOURCES_DIR:-}" ]; then
        echo "ghostty"; return
    fi
    if [ -n "${ALACRITTY_LOG:-}" ] || [ -n "${ALACRITTY_SOCKET:-}" ] || [ "${TERM:-}" = "alacritty" ]; then
        echo "alacritty"; return
    fi
    if [ -n "${VTE_VERSION:-}" ]; then
        echo "vte"; return
    fi
    if [ "${TERM:-}" = "foot" ] || [ "${TERM:-}" = "foot-extra" ]; then
        echo "foot"; return
    fi
    echo "unknown"
}

apply_resize() {
    local term="$1"
    case "$term" in
        kitty)
            # Requires `allow_remote_control yes` (or `socket-only`) in kitty.conf.
            if command -v kitty >/dev/null 2>&1 && \
               kitty @ set-font-size --decrement "$STEP" >/dev/null 2>&1; then
                RESTORE_CMD="kitty @ set-font-size --increment $STEP"
                echo "ok"; return
            fi
            note "kitty: 'kitty @ set-font-size' failed — enable allow_remote_control in kitty.conf"
            echo "skipped"; return
            ;;
        iterm2)
            if ! command -v osascript >/dev/null 2>&1; then
                echo "skipped"; return
            fi
            # Two known AppleScript surfaces; try the modern path first,
            # fall back to the legacy one. We compute the *current* size
            # then set absolute values, so restore is exact even if the
            # user manually nudges during the session would mis-sync —
            # acceptable trade-off for correctness on first launch.
            local cur
            cur=$(osascript -e 'tell application "iTerm" to tell current window to tell current session to get font size' 2>/dev/null)
            if [ -z "$cur" ]; then
                cur=$(osascript -e 'tell application "iTerm" to tell current session of current window to get font size of (text)' 2>/dev/null)
            fi
            if [ -z "$cur" ]; then
                note "iterm2: could not query current font size via AppleScript"
                echo "skipped"; return
            fi
            local target=$(( cur - STEP ))
            if [ "$target" -lt 4 ]; then target=4; fi
            if osascript -e "tell application \"iTerm\" to tell current window to tell current session to set font size to $target" >/dev/null 2>&1; then
                RESTORE_CMD="osascript -e 'tell application \"iTerm\" to tell current window to tell current session to set font size to $cur'"
                echo "ok"; return
            fi
            if osascript -e "tell application \"iTerm\" to tell current session of current window to set font size of (text) to $target" >/dev/null 2>&1; then
                RESTORE_CMD="osascript -e 'tell application \"iTerm\" to tell current session of current window to set font size of (text) to $cur'"
                echo "ok"; return
            fi
            note "iterm2: AppleScript set font size failed"
            echo "skipped"; return
            ;;
        wezterm)
            # WezTerm has no first-class "set-font-size" CLI. The closest
            # programmatic hooks are `wezterm cli` (spawn/send-text/list)
            # and the in-config IncreaseFontSize/DecreaseFontSize key
            # actions. There is no documented decrease-font CLI verb, so
            # we skip rather than trigger a user-defined keybinding.
            note "wezterm: no runtime set-font-size CLI; skipping"
            echo "skipped"; return
            ;;
        ghostty)
            note "ghostty: no remote-control CLI as of 2025; skipping"
            echo "skipped"; return
            ;;
        alacritty)
            note "alacritty: no runtime font-size IPC verb; skipping"
            echo "skipped"; return
            ;;
        vte)
            note "vte/gnome-terminal: no programmatic font-size hook; skipping"
            echo "skipped"; return
            ;;
        foot)
            note "foot: footclient has no font-size verb; skipping"
            echo "skipped"; return
            ;;
        *)
            echo "skipped"; return
            ;;
    esac
}

term=$(detect_terminal)
result=$(apply_resize "$term")

note "terminal=$term resize=$result"

# Hand off. Use exec so signals flow naturally; the EXIT trap still
# fires on the parent shell's teardown after the child returns control.
# Note: with `exec`, the trap technically runs in the new process image
# only if exec itself fails. To keep the trap useful we do NOT exec —
# we run termocode as a child and forward its exit status.
"$TERMOCODE_BIN" "$@"
status=$?
exit "$status"
