package app

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The user's own commands from the web (WEBP-7): the page's User
// Commands form lists commands.json and saves the whole list, the same
// file the laptop's Open Config: User Commands opens. A file that does
// not parse is never written over.

func init() {
	webOps[webproto.OpUserCmdsList] = (*Model).webUserCmdsList
	webOps[webproto.OpUserCmdsSave] = (*Model).webUserCmdsSave
}

// readUserCmdsFile reads commands.json: no file is an empty list; a
// file that does not parse is an error.
func readUserCmdsFile() (string, []UserCommand, error) {
	path, err := userCommandsPath()
	if err != nil {
		return "", nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return path, nil, nil
	}
	if err != nil {
		return path, nil, err
	}
	var cmds []UserCommand
	if err := json.Unmarshal(data, &cmds); err != nil {
		return path, nil, err
	}
	return path, cmds, nil
}

func userCmdList(cmds []UserCommand) webproto.UserCmdList {
	out := webproto.UserCmdList{Items: []webproto.UserCmd{}}
	for _, c := range cmds {
		out.Items = append(out.Items, webproto.UserCmd{ID: webName(c.ID), Title: webName(c.Title), Cmd: webName(c.Cmd), CWD: webName(c.CWD)})
	}
	return out
}

func (m *Model) webUserCmdsList(call *webCall) tea.Cmd {
	_, cmds, err := readUserCmdsFile()
	if err != nil {
		call.reply(webproto.UserCmdList{Items: []webproto.UserCmd{}, Error: "commands.json does not parse; fix it on the laptop (Open Config: User Commands)."})
		return nil
	}
	call.reply(userCmdList(cmds))
	return nil
}

func (m *Model) webUserCmdsSave(call *webCall) tea.Cmd {
	path, _, err := readUserCmdsFile()
	if err != nil {
		call.fail(webproto.CodeConflict, "commands.json does not parse; fix it on the laptop first, so nothing in it is lost.")
		return nil
	}
	cmds := []UserCommand{}
	for _, c := range call.args.(*webproto.UserCmdSaveArgs).Items {
		cmds = append(cmds, UserCommand{ID: c.ID, Title: c.Title, Cmd: c.Cmd, CWD: c.CWD})
	}
	data, err := json.MarshalIndent(cmds, "", "  ")
	if err == nil {
		err = os.MkdirAll(filepath.Dir(path), 0o755)
	}
	if err == nil {
		err = importAtomicWrite(path, append(data, '\n'), 0o644)
	}
	if err != nil {
		call.fail(webproto.CodeInternal, "The laptop could not save commands.json.")
		return nil
	}
	call.reply(userCmdList(cmds))
	return nil
}
