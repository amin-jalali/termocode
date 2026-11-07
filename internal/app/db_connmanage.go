package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Connection management (DBX-50). Databases: Manage Connection… opens
// the actions of the connection under the tree cursor: Edit… (a short
// form: name, host, port, user, database), Comment…, Rename…,
// Duplicate, Copy as JSON, Copy SSL from…, Use SSH Profile…, Save SSH as
// Profile…, Save as Template…, Show Driver and Server Version and, last,
// Delete… (a Cancel-first card). Connections from the project file can
// only be duplicated into your own list, copied and saved as templates.
// Databases: Paste Connection from Clipboard adds a copied connection;
// Databases: New Connection from Template… starts the add flow from a
// built-in or saved template. Profiles and templates live next to the
// connections file (db/ssh_profiles.json, db/templates.json).

const (
	pickerKindDBManage       pickerKindEnum  = 3920
	pickerKindDBCopySSL      pickerKindEnum  = 3924
	pickerKindDBSSHProfile   pickerKindEnum  = 3925
	pickerKindDBTemplate     pickerKindEnum  = 3929
	promptKindDBEditField    promptKindEnum  = 3921
	promptKindDBComment      promptKindEnum  = 3922
	promptKindDBRename       promptKindEnum  = 3923
	promptKindDBSSHProfileAs promptKindEnum  = 3926
	promptKindDBTemplateAs   promptKindEnum  = 3927
	confirmKindDBDeleteConn  confirmKindEnum = 3928
	pickerKindDBOptions      pickerKindEnum  = 3930
	promptKindDBOptionField  promptKindEnum  = 3931
)

// dbManageState is the connection being managed and the edit form draft.
type dbManageState struct {
	id    string
	edit  dbclient.ConnEdit
	step  int
	tpls  []dbclient.Conn
	profs []dbclient.SSHProfile
	// optField is the option the field prompt edits (DBX-28).
	optField string
}

type dbVersionMsg struct {
	id, driver, version string
	err                 error
}

// dbManageItems is the action menu; user is a connection of your own list.
func dbManageItems(user bool) []picker.Item {
	if !user {
		return []picker.Item{
			{ID: "duplicate", Title: "Duplicate into My Connections", Hint: "a copy you can change"},
			{ID: "copy-json", Title: "Copy as JSON", Hint: "no password"},
			{ID: "save-template", Title: "Save as Template…"},
			{ID: "version", Title: "Show Driver and Server Version"},
		}
	}
	return []picker.Item{
		{ID: "edit", Title: "Edit…", Hint: "name, host, port, user, database"},
		{ID: "comment", Title: "Comment…", Hint: "a note shown on the row"},
		{ID: "rename", Title: "Rename…", Hint: "change the id"},
		{ID: "options", Title: "Connection Options…", Hint: "schema, time zone, start SQL, filters"},
		{ID: "duplicate", Title: "Duplicate"},
		{ID: "copy-json", Title: "Copy as JSON", Hint: "no password"},
		{ID: "copy-ssl", Title: "Copy SSL Settings from…"},
		{ID: "ssh-profile", Title: "Use SSH Profile…"},
		{ID: "save-ssh-profile", Title: "Save SSH as Profile…"},
		{ID: "save-template", Title: "Save as Template…"},
		{ID: "version", Title: "Show Driver and Server Version"},
		{ID: "delete", Title: "Delete…", Hint: "asks first"},
	}
}

func dbUserFile(name string) (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "db", name), nil
}

func (m *Model) dbManage() *dbManageState {
	m.ensureDatabasesLoaded()
	if m.databases.manage == nil {
		m.databases.manage = &dbManageState{}
	}
	return m.databases.manage
}

// dbSaveUserConns writes your connections file and reloads the tree.
func (m *Model) dbSaveUserConns(users []dbclient.Conn, gone ...string) error {
	path, err := dbUserFile("connections.json")
	if err == nil {
		err = writeDBConfig(path, users)
	}
	if err != nil {
		return err
	}
	s := m.databases
	for _, id := range gone {
		delete(s.nodes, id)
	}
	s.userConns = users
	s.load(s.root)
	return nil
}

func (m *Model) dbManageFail(what string, err error) tea.Cmd {
	return m.pushToastDetail(toast.Errr, what, sanitizeDBText(err.Error()))
}

// databaseManageStart opens the action menu.
func (m *Model) databaseManageStart() tea.Cmd {
	s := m.databases
	r, ok := m.databaseCurrentRow()
	if s == nil || !ok {
		return tea.Batch(m.revealDatabasesView(false), m.pushToastDetail(toast.Info, "Select a connection first", ""))
	}
	c, ok := s.connection(r.conn)
	if !ok {
		return m.pushToastDetail(toast.Info, "Select a connection first", "")
	}
	ms := m.dbManage()
	*ms = dbManageState{id: c.ID}
	m.dbOpenPicker(pickerKindDBManage, " "+sanitizeDBText(dbConnName(c))+" ", dbManageItems(c.Source == dbclient.SourceUser))
	return nil
}

// dbManaged is the connection being managed.
func (m *Model) dbManaged() (dbclient.Conn, bool) {
	if m.databases == nil || m.databases.manage == nil {
		return dbclient.Conn{}, false
	}
	return m.databases.connection(m.databases.manage.id)
}

func (m *Model) dbManagePrompt(kind promptKindEnum, title, label, value string) tea.Cmd {
	m.prompt = prompt.New(title, label, value)
	m.prompt.SetAllowEmpty(true)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen, m.promptKind = true, kind
	return nil
}

// dbEditSteps are the form fields; SQLite has only the name.
var dbEditSteps = []string{"Name", "Host", "Port", "User", "Database"}

func (m *Model) dbEditAsk() tea.Cmd {
	ms := m.dbManage()
	e := ms.edit
	values := []string{e.Name, e.Host, strconv.Itoa(e.Port), e.User, e.Database}
	label := dbEditSteps[ms.step]
	return m.dbManagePrompt(promptKindDBEditField, "Edit connection "+sanitizeDBText(ms.id),
		label+" ("+strconv.Itoa(ms.step+1)+" of "+strconv.Itoa(m.dbEditCount())+"):", values[ms.step])
}

func (m *Model) dbEditCount() int {
	if c, ok := m.dbManaged(); ok && c.Driver == "sqlite" {
		return 1
	}
	return len(dbEditSteps)
}

// databaseManagePick runs one action of the menu.
func (m *Model) databaseManagePick(id string) tea.Cmd {
	c, ok := m.dbManaged()
	if !ok {
		return nil
	}
	s := m.databases
	ms := s.manage
	switch id {
	case "edit":
		ms.edit = dbclient.ConnEdit{Name: c.Name, Comment: c.Comment, Host: c.Host, Port: c.Port, User: c.User, Database: c.Database}
		ms.step = 0
		return m.dbEditAsk()
	case "options":
		m.dbOpenOptions(c)
		return nil
	case "comment":
		return m.dbManagePrompt(promptKindDBComment, "Comment on "+sanitizeDBText(dbConnName(c)), "Comment (empty removes it):", c.Comment)
	case "rename":
		return m.dbManagePrompt(promptKindDBRename, "Rename "+sanitizeDBText(c.ID), "New id (a-z, 0-9, - and _):", c.ID)
	case "duplicate":
		var users []dbclient.Conn
		var newID string
		var err error
		if c.Source == dbclient.SourceUser {
			users, newID, err = dbclient.DuplicateConn(s.userConns, c.ID)
		} else {
			var n dbclient.Conn
			n, err = dbclient.FromTemplate(s.conns, dbclient.TemplateOf(c, dbConnName(c)), c.ID)
			n.Name = dbConnName(c) + " (copy)"
			users, newID = append(append([]dbclient.Conn(nil), s.userConns...), n), n.ID
		}
		if err == nil {
			err = m.dbSaveUserConns(users)
		}
		if err != nil {
			return m.dbManageFail("Cannot duplicate the connection", err)
		}
		s.databaseSelectPath(newID)
		return m.pushToastDetail(toast.Info, "Duplicated as "+newID, "Its password is not copied; you are asked on the first connect.")
	case "copy-json":
		text, err := dbclient.ConnJSON(c)
		if err != nil {
			return m.dbManageFail("Cannot copy the connection", err)
		}
		return m.copyWithToast(text, "Connection copied as JSON (no password)")
	case "copy-ssl":
		var items []picker.Item
		for _, o := range s.conns {
			if o.ID != c.ID && o.SSL != nil {
				items = append(items, picker.Item{ID: o.ID, Title: sanitizeDBText(dbConnName(o)), Hint: "ssl " + sanitizeDBText(o.SSL.Mode)})
			}
		}
		if len(items) == 0 {
			return m.pushToastDetail(toast.Info, "No other connection has SSL settings", "")
		}
		m.dbOpenPicker(pickerKindDBCopySSL, " Copy SSL settings from… ", items)
		return nil
	case "ssh-profile":
		ps, err := dbLoadSSHProfiles()
		if err != nil {
			return m.dbManageFail("Cannot read the SSH profiles", err)
		}
		if len(ps) == 0 {
			return m.pushToastDetail(toast.Info, "No SSH profiles yet", "Use Save SSH as Profile… on a connection with an SSH tunnel.")
		}
		ms.profs = ps
		items := make([]picker.Item, 0, len(ps))
		for i, p := range ps {
			hint := p.SSH.Host
			if p.SSH.User != "" {
				hint = p.SSH.User + "@" + hint
			}
			items = append(items, picker.Item{ID: strconv.Itoa(i), Title: sanitizeDBText(p.Name), Hint: sanitizeDBText(hint)})
		}
		m.dbOpenPicker(pickerKindDBSSHProfile, " Use SSH profile ", items)
		return nil
	case "save-ssh-profile":
		if c.SSH == nil {
			return m.pushToastDetail(toast.Info, "This connection has no SSH tunnel", "Add one with Databases: Add SSH Tunnel… first.")
		}
		return m.dbManagePrompt(promptKindDBSSHProfileAs, "Save SSH as profile", "Profile name:", c.SSH.Host)
	case "save-template":
		return m.dbManagePrompt(promptKindDBTemplateAs, "Save as template", "Template name:", dbConnName(c))
	case "version":
		return m.databaseVersion(c)
	case "delete":
		m.confirm = confirm.New("Delete connection "+sanitizeDBText(dbConnName(c))+"?",
			"This removes "+sanitizeDBText(c.ID)+" from your connections file, closes its sessions and deletes its saved password. The database itself is not touched.",
			[]confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "delete", Title: "Delete", Style: confirm.StyleDestructive}})
		m.confirm.SetSize(m.w, m.h)
		m.confirmOpen, m.confirmKind = true, confirmKindDBDeleteConn
		return nil
	}
	return nil
}

// dbCloseConnSessions closes the sessions and the tunnel of id.
func (s *dbState) dbCloseConnSessions(id string) {
	for key, sess := range s.sessions {
		if key == id || strings.HasPrefix(key, id+"/") {
			_ = sess.Close()
			delete(s.sessions, key)
		}
	}
	s.closeTunnel(id)
}

// databaseDeleteConn removes the managed connection.
func (m *Model) databaseDeleteConn() tea.Cmd {
	c, ok := m.dbManaged()
	if !ok || c.Source != dbclient.SourceUser {
		return nil
	}
	s := m.databases
	users, err := dbclient.DeleteConn(s.userConns, c.ID)
	if err != nil {
		return m.dbManageFail("Cannot delete the connection", err)
	}
	s.dbCloseConnSessions(c.ID)
	var gone []string
	for p := range s.nodes {
		if p == c.ID || strings.HasPrefix(p, c.ID+"/") {
			gone = append(gone, p)
		}
	}
	if err := m.dbSaveUserConns(users, gone...); err != nil {
		return m.dbManageFail("Cannot delete the connection", err)
	}
	if store := databaseSecretStore(); store != nil && !store.Locked() {
		for _, name := range []string{"db." + c.ID, dbSSHSecretName(c.ID, "password"), dbSSHSecretName(c.ID, "passphrase")} {
			_ = store.Delete(name)
		}
	}
	s.manage = nil
	return m.pushToast(toast.Info, "Deleted connection "+sanitizeDBText(c.ID))
}

// databaseRenameConn changes the id; the saved password moves with it.
func (m *Model) databaseRenameConn(newID string) tea.Cmd {
	c, ok := m.dbManaged()
	newID = strings.TrimSpace(newID)
	if !ok || newID == "" || newID == c.ID {
		return nil
	}
	s := m.databases
	users, err := dbclient.RenameConn(s.userConns, c.ID, newID)
	if err == nil && s.duplicateConnectionID(newID) {
		err = errors.New("connection id " + newID + " is already used")
	}
	if err != nil {
		return m.dbManageFail("Cannot rename the connection", err)
	}
	if store := databaseSecretStore(); store != nil && !store.Locked() {
		for _, pair := range [][2]string{
			{"db." + c.ID, "db." + newID},
			{dbSSHSecretName(c.ID, "password"), dbSSHSecretName(newID, "password")},
			{dbSSHSecretName(c.ID, "passphrase"), dbSSHSecretName(newID, "passphrase")},
		} {
			if v, ok, err := store.Get(pair[0]); err == nil && ok {
				if store.Set(pair[1], v) == nil {
					_ = store.Delete(pair[0])
				}
			}
		}
		for i := range users {
			if users[i].ID == newID && users[i].Password == "${secret:db."+c.ID+"}" {
				users[i].Password = "${secret:db." + newID + "}"
			}
		}
	}
	s.dbCloseConnSessions(c.ID)
	if err := m.dbSaveUserConns(users, c.ID); err != nil {
		return m.dbManageFail("Cannot rename the connection", err)
	}
	s.manage.id = newID
	s.databaseSelectPath(newID)
	return m.pushToast(toast.Info, "Renamed to "+sanitizeDBText(newID))
}

// dbApplyEdit saves the edit form.
func (m *Model) dbApplyEdit() tea.Cmd {
	ms := m.dbManage()
	users, err := dbclient.EditConn(m.databases.userConns, ms.id, ms.edit)
	if err == nil {
		err = m.dbSaveUserConns(users)
	}
	if err != nil {
		return m.dbManageFail("Cannot save the connection", err)
	}
	note := ""
	if _, open := m.databases.sessions[ms.id]; open {
		note = "Connect again to use the new settings."
	}
	return m.pushToastDetail(toast.Info, "Connection saved", note)
}

// databaseManagePrompt routes the prompts of this file.
func (m *Model) databaseManagePrompt(kind promptKindEnum, value string) (tea.Cmd, bool) {
	switch kind {
	case promptKindDBOptionField:
		return m.dbApplyOption(value), true
	case promptKindDBEditField, promptKindDBComment, promptKindDBRename, promptKindDBSSHProfileAs, promptKindDBTemplateAs:
	default:
		return nil, false
	}
	c, ok := m.dbManaged()
	if !ok {
		return nil, true
	}
	ms := m.databases.manage
	value = strings.TrimSpace(value)
	switch kind {
	case promptKindDBEditField:
		switch ms.step {
		case 0:
			ms.edit.Name = value
		case 1:
			ms.edit.Host = value
		case 2:
			p, err := strconv.Atoi(value)
			if err != nil || p < 1 || p > 65535 {
				cmd := m.dbEditAsk()
				m.prompt.SetError("A port is a number from 1 to 65535")
				return cmd, true
			}
			ms.edit.Port = p
		case 3:
			ms.edit.User = value
		case 4:
			ms.edit.Database = value
		}
		ms.step++
		if ms.step < m.dbEditCount() {
			return m.dbEditAsk(), true
		}
		return m.dbApplyEdit(), true
	case promptKindDBComment:
		ms.edit = dbclient.ConnEdit{Name: c.Name, Comment: value, Host: c.Host, Port: c.Port, User: c.User, Database: c.Database}
		return m.dbApplyEdit(), true
	case promptKindDBRename:
		return m.databaseRenameConn(value), true
	case promptKindDBSSHProfileAs:
		if value == "" || c.SSH == nil {
			return nil, true
		}
		ps, err := dbLoadSSHProfiles()
		if err == nil {
			err = dbWriteUserJSON("ssh_profiles.json", func() ([]byte, error) {
				return dbclient.SSHProfilesJSON(dbclient.SaveSSHProfile(ps, dbclient.SSHProfile{Name: value, SSH: *c.SSH}))
			})
		}
		if err != nil {
			return m.dbManageFail("Cannot save the SSH profile", err), true
		}
		return m.pushToast(toast.Info, "SSH profile "+sanitizeDBText(value)+" saved"), true
	case promptKindDBTemplateAs:
		if value == "" {
			return nil, true
		}
		ts, err := dbLoadTemplates()
		if err == nil {
			t := dbclient.TemplateOf(c, value)
			kept := ts[:0]
			for _, x := range ts {
				if x.ID != t.ID {
					kept = append(kept, x)
				}
			}
			err = dbWriteUserJSON("templates.json", func() ([]byte, error) { return dbclient.TemplatesJSON(append(kept, t)) })
		}
		if err != nil {
			return m.dbManageFail("Cannot save the template", err), true
		}
		return m.pushToastDetail(toast.Info, "Template "+sanitizeDBText(value)+" saved", "Databases: New Connection from Template… offers it."), true
	}
	return nil, true
}

func dbWriteUserJSON(name string, data func() ([]byte, error)) error {
	path, err := dbUserFile(name)
	if err != nil {
		return err
	}
	b, err := data()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func dbReadUserJSON(name string) ([]byte, error) {
	path, err := dbUserFile(name)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return b, err
}

func dbLoadSSHProfiles() ([]dbclient.SSHProfile, error) {
	b, err := dbReadUserJSON("ssh_profiles.json")
	if err != nil || b == nil {
		return nil, err
	}
	return dbclient.ParseSSHProfiles(b)
}

func dbLoadTemplates() ([]dbclient.Conn, error) {
	b, err := dbReadUserJSON("templates.json")
	if err != nil || b == nil {
		return nil, err
	}
	return dbclient.ParseTemplates(b)
}

// databaseVersion reads the server version on a read-only session.
func (m *Model) databaseVersion(c dbclient.Conn) tea.Cmd {
	q := dbclient.ServerVersionQuery(c.Driver)
	sess := m.databases.sessions[c.ID]
	if q == "" || sess == nil {
		detail := "Connect first (Enter on the connection) to read the server version."
		if q == "" {
			detail = "Databases: Show Server Info shows the Redis version."
		}
		return m.pushToastDetail(toast.Info, "Driver: "+sanitizeDBText(c.Driver), detail)
	}
	id, driver := c.ID, c.Driver
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), dbTestTimeout)
		defer cancel()
		ro, err := sess.ReadOnlySession()
		if err != nil {
			return dbVersionMsg{id: id, driver: driver, err: err}
		}
		defer ro.Close()
		r := ro.Run(ctx, q, dbclient.RunOptions{MaxRows: 1})
		if r.Err != nil {
			return dbVersionMsg{id: id, driver: driver, err: r.Err}
		}
		v := ""
		if len(r.Rows) > 0 && len(r.Rows[0]) > 0 {
			v = r.Rows[0][0].Text
		}
		return dbVersionMsg{id: id, driver: driver, version: v}
	}
}

func (m *Model) onDBVersion(msg dbVersionMsg) tea.Cmd {
	if msg.err != nil {
		return m.pushToastDetail(toast.Errr, "Could not read the server version", dbclient.Redact(sanitizeDBText(msg.err.Error())))
	}
	return m.pushToastDetail(toast.Info, "Driver: "+sanitizeDBText(msg.driver), "Server: "+sanitizeDBText(msg.version))
}

// databasePasteConn adds the connection JSON on the clipboard.
func (m *Model) databasePasteConn() tea.Cmd {
	m.ensureDatabasesLoaded()
	s := m.databases
	c, err := dbclient.ParseConnJSON(readClipboardText(), s.conns)
	if err != nil {
		return m.dbManageFail("The clipboard has no connection", err)
	}
	if err := m.dbSaveUserConns(append(append([]dbclient.Conn(nil), s.userConns...), c)); err != nil {
		return m.dbManageFail("Cannot add the connection", err)
	}
	s.databaseSelectPath(c.ID)
	return tea.Batch(m.revealDatabasesView(false), m.pushToastDetail(toast.Info, "Added "+sanitizeDBText(c.ID), "A password is asked on the first connect."))
}

// databaseTemplateStart lists the built-in and your saved templates.
func (m *Model) databaseTemplateStart() tea.Cmd {
	m.ensureDatabasesLoaded()
	mine, err := dbLoadTemplates()
	if err != nil {
		return m.dbManageFail("Cannot read your templates", err)
	}
	all := append(append([]dbclient.Conn(nil), dbclient.BuiltinTemplates...), mine...)
	ms := m.dbManage()
	*ms = dbManageState{tpls: all}
	items := make([]picker.Item, 0, len(all))
	for i, t := range all {
		hint := "built-in · " + t.Driver
		if i >= len(dbclient.BuiltinTemplates) {
			hint = "yours · " + t.Driver
		}
		items = append(items, picker.Item{ID: strconv.Itoa(i), Title: sanitizeDBText(dbConnName(t)), Hint: sanitizeDBText(hint)})
	}
	m.dbOpenPicker(pickerKindDBTemplate, " New connection from template ", items)
	return nil
}

// databaseTemplatePick starts the add flow (password, test, save).
func (m *Model) databaseTemplatePick(id string) tea.Cmd {
	ms := m.dbManage()
	i, err := strconv.Atoi(id)
	if err != nil || i < 0 || i >= len(ms.tpls) {
		return nil
	}
	s := m.databases
	c, err := dbclient.FromTemplate(s.conns, ms.tpls[i], ms.tpls[i].ID)
	if err != nil {
		return m.dbManageFail("Cannot use the template", err)
	}
	s.pendingConn, s.pendingPassword, s.addFromURL = c, "", false
	return m.databasePromptPasswordForPending()
}

// databaseManagePicker routes the pickers of this file.
func (m *Model) databaseManagePicker(id string) (tea.Cmd, bool) {
	switch m.pickerKind {
	case pickerKindDBManage:
		return m.databaseManagePick(id), true
	case pickerKindDBTemplate:
		return m.databaseTemplatePick(id), true
	case pickerKindDBOptions:
		return m.dbOptionAsk(id), true
	case pickerKindDBCopySSL, pickerKindDBSSHProfile:
	default:
		return nil, false
	}
	c, ok := m.dbManaged()
	if !ok {
		return nil, true
	}
	s := m.databases
	var users []dbclient.Conn
	var err error
	if m.pickerKind == pickerKindDBCopySSL {
		from, ok := s.connection(id)
		if !ok || from.SSL == nil {
			return nil, true
		}
		users = append([]dbclient.Conn(nil), s.userConns...)
		for i := range users {
			if users[i].ID == c.ID {
				ssl := *from.SSL
				users[i].SSL = &ssl
			}
		}
	} else {
		i, e := strconv.Atoi(id)
		if e != nil || i < 0 || i >= len(s.manage.profs) {
			return nil, true
		}
		users, err = dbclient.UseSSHProfile(s.userConns, c.ID, s.manage.profs[i])
	}
	if err == nil {
		err = m.dbSaveUserConns(users)
	}
	if err != nil {
		return m.dbManageFail("Cannot save the connection", err), true
	}
	return m.pushToastDetail(toast.Info, "Connection saved", "Connect again to use the new settings."), true
}

// databaseManageConfirm routes the delete card.
func (m *Model) databaseManageConfirm(id string) (tea.Cmd, bool) {
	if m.confirmKind != confirmKindDBDeleteConn {
		return nil, false
	}
	if id != "delete" {
		return nil, true
	}
	return m.databaseDeleteConn(), true
}
