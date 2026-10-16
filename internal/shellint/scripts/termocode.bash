# termocode shell integration for bash (OSC 133 + OSC 7).
# termocode starts bash with --init-file pointing here, so this file
# first loads what bash loads anyway (~/.bashrc), then adds small hooks
# that tell termocode where a prompt starts, which command runs, its
# exit code and the current folder. Nothing here changes how the shell
# behaves. Set "terminal_shell_integration": false in config.json to
# turn it off.

if [ -r "$HOME/.bashrc" ]; then
	. "$HOME/.bashrc"
fi

# Interactive bash only, once (an rc file may source this again).
if [ -z "${__tc_si-}" ] && [[ $- == *i* ]]; then
	__tc_si=1
	unset TERMOCODE_SHELL_INTEGRATION

	# __tc_enc puts $1 into __tc_r with the characters that would end or
	# split an OSC sequence percent-encoded (no subshell).
	__tc_enc() {
		__tc_r=${1//%/%25}
		__tc_r=${__tc_r//;/%3B}
		__tc_r=${__tc_r//$'\n'/%0A}
		__tc_r=${__tc_r//$'\r'/%0D}
		__tc_r=${__tc_r//$'\a'/%07}
		__tc_r=${__tc_r//$'\e'/%1B}
	}

	# Runs first in PROMPT_COMMAND: command end (D with the exit code),
	# the folder (OSC 7) and prompt start (A). It keeps $? for the
	# user's own prompt.
	__tc_precmd() {
		local __tc_st=$?
		if [ -n "${__tc_ran+x}" ]; then
			unset __tc_ran
			builtin printf '\e]133;D;%s\a' "$__tc_st"
		fi
		__tc_enc "$PWD"
		builtin printf '\e]7;file://%s%s\a\e]133;A\a' "${HOSTNAME-}" "$__tc_r"
		__tc_hn=$HISTCMD
		case $PS1 in
		*'133;B'*) ;;
		*) PS1=$PS1'\[\e]133;B\a\]' ;;
		esac
		return $__tc_st
	}

	# Printed by PS0 (bash 4.4+) when a command line was read: command
	# start (C) with the command text from history. When history did not
	# keep the line (HISTCONTROL=ignorespace, history off), the text is
	# left empty instead of naming the previous command.
	__tc_preexec() {
		local h n
		h=$(HISTTIMEFORMAT= builtin history 1)
		n=${h#"${h%%[![:space:]]*}"}
		n=${n%%[!0-9]*}
		h=${h#*[0-9] }
		h=${h#"${h%%[![:space:]]*}"}
		if [ -z "$n" ] || [ "$n" != "${__tc_hn-}" ]; then
			h=
		fi
		__tc_enc "$h"
		builtin printf '\e]133;C;cmdline_url=%s\a' "$__tc_r"
	}

	if [ -z "${TERMOCODE_SI_FORCE_DEBUG-}" ] && { [ "${BASH_VERSINFO[0]}" -gt 4 ] || { [ "${BASH_VERSINFO[0]}" -eq 4 ] && [ "${BASH_VERSINFO[1]}" -ge 4 ]; }; }; then
		# ${__tc_ran=} marks "a command ran" in this shell (PS0 expands
		# here, not in a subshell); $(...) prints the C sequence.
		PS0=${PS0-}'${__tc_ran=}$(__tc_preexec)'
	elif [ -z "$(builtin trap -p DEBUG)" ]; then
		# bash before 4.4 (macOS ships 3.2) has no PS0: the DEBUG trap
		# runs before every command, so it reports only the first one
		# after a prompt (__tc_ready is set as the last step of
		# PROMPT_COMMAND). A DEBUG trap of the user's own is left alone.
		__tc_debug() {
			[ -n "${__tc_ready-}" ] || return 0
			[ -z "${COMP_LINE-}" ] || return 0
			unset __tc_ready
			__tc_ran=
			__tc_preexec
		}
		builtin trap '__tc_debug' DEBUG
		__tc_tail=1
	fi

	if [[ "$(builtin declare -p PROMPT_COMMAND 2>/dev/null)" == "declare -a"* ]]; then
		PROMPT_COMMAND=(__tc_precmd "${PROMPT_COMMAND[@]}")
		[ -z "${__tc_tail-}" ] || PROMPT_COMMAND+=('__tc_ready=1')
	else
		PROMPT_COMMAND="__tc_precmd${PROMPT_COMMAND:+;$PROMPT_COMMAND}"
		[ -z "${__tc_tail-}" ] || PROMPT_COMMAND="$PROMPT_COMMAND;__tc_ready=1"
	fi
fi
