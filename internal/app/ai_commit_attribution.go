package app

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Commit attribution (CMP-100): who is named on a commit the AI makes —
// the chat's git_commit and the commit of a plan run in a worktree.
// git_ai_attribution in config.json:
//
//   - user (default): the commit is the user's; nothing is added.
//   - co-author: the user is the author; a Co-authored-by trailer names
//     the AI.
//   - ai: the AI is the author; the user stays the committer.
//
// Only the user picks it (the AI may not change it).

const aiAttributionRowID = "setting-git-ai-attribution"

// aiCommitIdentity is the name and e-mail the AI gets on commits.
const aiCommitIdentity = "termocode AI <ai@termocode.invalid>"

var aiAttributionChoices = []string{"user", "co-author", "ai"}

func init() {
	settingsRows = append(settingsRows, settingsRow{ID: aiAttributionRowID, Label: "git_ai_attribution", Kind: "enum", Default: "user",
		Desc: "Who is named on a commit the AI makes (git_commit in the chat, a plan run in a worktree): user (only you), co-author (you, with a Co-authored-by trailer for termocode AI) or ai (termocode AI is the author, you are the committer)."})
	aiSettingPolicies["git_ai_attribution"] = aiNever("who is named on the AI's commits is the user's choice")
}

// aiCommitAttribution is the saved mode ("user" when unset or unknown).
func aiCommitAttribution() string {
	path, err := settingsConfigPath()
	if err != nil {
		return "user"
	}
	var s struct {
		Mode string `json:"git_ai_attribution"`
	}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	for _, c := range aiAttributionChoices {
		if s.Mode == c {
			return c
		}
	}
	return "user"
}

// aiCommitAttributed is message and the --author of an AI commit under
// the saved mode (author "" = the user).
func aiCommitAttributed(message string) (string, string) {
	switch aiCommitAttribution() {
	case "co-author":
		return git.AddTrailer(message, "Co-authored-by", aiCommitIdentity), ""
	case "ai":
		return message, aiCommitIdentity
	}
	return message, ""
}

// aiAttributionNote is what the approval card adds to "Commit: …".
func aiAttributionNote() string {
	switch aiCommitAttribution() {
	case "co-author":
		return " (co-author: termocode AI)"
	case "ai":
		return " (author: termocode AI)"
	}
	return ""
}

// aiAttributionTools makes the chat's git_commit follow the setting.
func aiAttributionTools(tools []ai.Tool, root string) []ai.Tool {
	for i, t := range tools {
		if t.Name != "git_commit" {
			continue
		}
		describe := t.Describe
		tools[i].Describe = func(a map[string]any) string {
			h := ""
			if describe != nil {
				h = describe(a)
			}
			return h + aiAttributionNote()
		}
		tools[i].Run = func(_ context.Context, a map[string]any) (string, error) {
			amend, msg, author := ai.ArgBool(a, "amend"), ai.ArgString(a, "message"), ""
			if !amend || strings.TrimSpace(msg) != "" { // an amend with no message keeps the old one as it is
				msg, author = aiCommitAttributed(msg)
			}
			if err := git.CommitOpts(root, msg, git.CommitOptions{Author: author, Amend: amend, Signoff: ai.ArgBool(a, "signoff")}); err != nil {
				return "", err
			}
			switch {
			case amend:
				return "amended the last commit", nil
			case author != "":
				return "committed (author: termocode AI, committer: the user)", nil
			case msg != ai.ArgString(a, "message"):
				return "committed (with a Co-authored-by trailer for termocode AI)", nil
			}
			return "committed", nil
		}
	}
	return tools
}

// aiCommitPlanWorktree commits a plan run's worktree under the setting.
func aiCommitPlanWorktree(dir, id, message string) (bool, error) {
	msg, author := aiCommitAttributed(message)
	return git.CommitAgentWorktreeAs(dir, id, msg, author)
}

// aiAttributionSettingValue renders the row. ok=false: not ours.
func aiAttributionSettingValue(id string) (string, bool) {
	if id != aiAttributionRowID {
		return "", false
	}
	return aiCommitAttribution(), true
}

// applyAIAttributionSetting checks and saves the row. ok=false: not ours.
func (m *Model) applyAIAttributionSetting(id, value string) (tea.Cmd, bool) {
	if id != aiAttributionRowID {
		return nil, false
	}
	value = strings.TrimSpace(value)
	ok := false
	for _, c := range aiAttributionChoices {
		ok = ok || c == value
	}
	if !ok {
		return m.pushToastDetail(toast.Errr, "git_ai_attribution: expected "+strings.Join(aiAttributionChoices, ", "), ""), true
	}
	if err := saveHTTPSetting("git_ai_attribution", value); err != nil {
		return m.pushToastDetail(toast.Errr, "Settings error", err.Error()), true
	}
	return m.pushToastDetail(toast.Info, "git_ai_attribution = "+value, ""), true
}
