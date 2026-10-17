# termocode shell integration for fish (OSC 133 + OSC 7). termocode puts
# this folder in front of XDG_DATA_DIRS, so fish loads this file with its
# other vendor files (the user's config.fish still loads as usual). It
# tells termocode where a prompt starts, which command runs, its exit
# code and the current folder. Set "terminal_shell_integration": false in
# config.json to turn it off.

# Put XDG_DATA_DIRS back for programs started from this shell.
if set -q TERMOCODE_XDG_DATA_DIRS
    if test -n "$TERMOCODE_XDG_DATA_DIRS"
        set -gx XDG_DATA_DIRS $TERMOCODE_XDG_DATA_DIRS
    else
        set -e XDG_DATA_DIRS
    end
    set -e TERMOCODE_XDG_DATA_DIRS
end
set -e TERMOCODE_SHELL_INTEGRATION

if status is-interactive; and not set -q __tc_si
    set -g __tc_si 1

    function __tc_enc
        string replace -a '%' '%25' -- $argv[1] | string replace -a ';' '%3B' | string join '%0A'
    end

    function __tc_preexec --on-event fish_preexec
        set -g __tc_ran 1
        printf '\e]133;C;cmdline_url=%s\a' (__tc_enc $argv[1])
    end

    function __tc_postexec --on-event fish_postexec
        set -l st $status
        if set -q __tc_ran
            set -e __tc_ran
            printf '\e]133;D;%s\a' $st
        end
    end

    function __tc_prompt --on-event fish_prompt
        printf '\e]7;file://%s%s\a\e]133;A\a' $hostname (__tc_enc $PWD)
    end
end
