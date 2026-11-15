package app

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// "Write it for me" in the phone's commit sheet (git.ai_message): the
// laptop's Generate Commit Message with AI (git_ai_helpers.go) for the
// web. The staged diff goes to the laptop's AI once, with the recent
// subjects for their style; the answer goes back to the page's box.
// Nothing is committed here.

func init() {
	webOps[webproto.OpGitAIMessage] = (*Model).webGitAIMessage
}

func (m *Model) webGitAIMessage(call *webCall) tea.Cmd {
	root, ok := m.webGitRoot(call)
	if !ok {
		return nil
	}
	if !m.aiConfigured() {
		call.fail(webproto.CodeConflict, "AI is not set up on the laptop.")
		return nil
	}
	s := m.aiS()
	prov, model := s.provider, s.cfg.InlineModel
	call.later()
	return func() tea.Msg {
		diff, cut, err := git.ChangesDiff(root, true, gitAIDiffMax)
		if err != nil {
			webGitFail(call, err)
			return nil
		}
		if strings.TrimSpace(diff) == "" {
			call.fail(webproto.CodeConflict, "Stage a file first.")
			return nil
		}
		recent, _ := git.RecentMessages(root, 10)
		ctx, cancel := context.WithTimeout(call.ctx, gitAITimeout)
		defer cancel()
		text, last := ai.Collect(prov.Stream(ctx, ai.Request{Messages: gitAIMessagePrompt("commit", diff, cut, recent), Model: model, MaxTokens: gitAIMaxTokens}))
		msg := gitAICleanReply(text)
		if last.Err != nil || msg == "" {
			if last.Err != nil && ctx.Err() == nil {
				recordError("[web] git.ai_message: " + ai.Redact(last.Err.Error()))
			}
			call.fail(webproto.CodeBusy, "The AI did not answer. Try again.")
			return nil
		}
		call.reply(webproto.GitAIMessage{Message: msg})
		return nil
	}
}
