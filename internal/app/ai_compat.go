package app

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Setups from other apps (OMX-11). Agent files (.claude/agents/*.md) are
// project profiles: they get the project profile card before a first
// use. Command files (.claude/commands/*.md, and the user's own in
// <config>/termocode/commands and ~/.claude/commands) are "/name" chat
// commands; a project's commands share the project skills card (their
// hash joins the skills hash, so one answer covers both).

// aiCommandsUserDirs are the user's own command folders.
func aiCommandsUserDirs() []string {
	var dirs []string
	if dir, err := configDir(); err == nil {
		dirs = append(dirs, filepath.Join(dir, "commands"))
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		dirs = append(dirs, filepath.Join(home, ".claude", "commands"))
	}
	return dirs
}

// aiLoadChatCommands reads the chat commands of root and of the user.
func aiLoadChatCommands(root string) ai.ChatCommands {
	return ai.LoadChatCommands(root, aiCommandsUserDirs()...)
}

// aiFoldCommandsTrust joins the project commands' hash into the project
// skills hash (one card covers both) and keeps the count for the card.
func aiFoldCommandsTrust(s *ai.Skills, c ai.ChatCommands) {
	if c.ProjectHash == "" {
		return
	}
	h := sha256.Sum256([]byte(s.ProjectHash + "\x00commands\x00" + c.ProjectHash))
	s.ProjectHash = hex.EncodeToString(h[:])
}

// aiProjectCommands are the project's commands of c.
func aiProjectCommands(c ai.ChatCommands) []ai.ChatCommand {
	var out []ai.ChatCommand
	for _, x := range c.List {
		if x.Source == ai.SkillSourceProject {
			out = append(out, x)
		}
	}
	return out
}

// aiExpandChatCommand turns "/name args" into the command's text. ok is
// false when text is no known command (it is sent as it is). A project
// command that is not trusted gives a problem text instead.
func aiExpandChatCommand(root, text string) (out string, ok bool, problem string) {
	name, args, isCmd := ai.SplitChatCommand(text)
	if !isCmd {
		return text, false, ""
	}
	c, found := aiLoadChatCommands(root).Find(name)
	if !found {
		return text, false, ""
	}
	if c.Source == ai.SkillSourceProject && aiSkillsTrust(aiLoadSkills(root)) != aiTrustUse {
		return text, false, "/" + name + " is a project command: trust the project skills first (AI: Skills…)"
	}
	return c.Expand(args), true, ""
}

// aiAgentFileProfiles adds the agent files of root to set as project
// profiles (a name the set has already stays as it is).
func aiAgentFileProfiles(set *ai.ProfileSet, root string) {
	ps, notes := ai.LoadAgentProfiles(root)
	for _, p := range ps {
		if _, taken := set.Get(p.Name); !taken {
			set.Add(p)
		}
	}
	for _, n := range notes {
		set.Errors = append(set.Errors, "skipped "+n)
	}
}
