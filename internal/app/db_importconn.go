package app

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Import connections (DBX-22): Databases: Import Connections… lists the
// files other tools keep (~/.pgpass, ~/.my.cnf) that exist, or takes a
// typed path (a pgpass file, a MySQL option file or one URL per line). A
// card lists what was found (never a password) with Cancel first; Import
// saves them to the user's connections file, each password in the secret
// store as ${secret:db.<id>}.

const (
	pickerKindDBImportConn     pickerKindEnum  = 5240
	promptKindDBImportConnPath promptKindEnum  = 5240
	confirmKindDBImportConn    confirmKindEnum = 5240
)

const dbImportConnTypePath = "\x00path"

// dbImportConnMax caps the file read (these files are small).
const dbImportConnMax = 1 << 20

// dbImportConnFiles are the well-known files, relative to the home folder.
var dbImportConnFiles = []struct{ rel, hint string }{
	{".pgpass", "PostgreSQL password file"},
	{".my.cnf", "MySQL option file"},
}

func (m *Model) databaseImportConnStart() tea.Cmd {
	m.ensureDatabasesLoaded()
	var items []picker.Item
	if home, err := os.UserHomeDir(); err == nil {
		for _, f := range dbImportConnFiles {
			p := filepath.Join(home, f.rel)
			if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
				items = append(items, picker.Item{ID: p, Title: "~/" + f.rel, Hint: f.hint})
			}
		}
	}
	items = append(items, picker.Item{ID: dbImportConnTypePath, Title: "Type a path…", Hint: "pgpass, my.cnf / .ini, or one URL per line"})
	m.picker = picker.NewItems("Import connections from", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBImportConn
	m.pickerOpen = true
	return nil
}

// databaseImportConnPick reads the picked file, or asks for a path.
func (m *Model) databaseImportConnPick(id string) (tea.Cmd, bool) {
	if m.pickerKind != pickerKindDBImportConn {
		return nil, false
	}
	if id == dbImportConnTypePath {
		return m.openHTTPPrompt(promptKindDBImportConnPath, "Import connections", "File (pgpass, my.cnf / .ini, or one URL per line):", ""), true
	}
	return m.databaseImportConnRead(id), true
}

// databaseImportConnPrompt takes the typed path. ok=false: not ours.
func (m *Model) databaseImportConnPrompt(kind promptKindEnum, value string) (tea.Cmd, bool) {
	if kind != promptKindDBImportConnPath {
		return nil, false
	}
	abs, _, err := m.dbResolvePath(value)
	if err != nil {
		return m.pushToastDetail(toast.Warn, "Cannot import", err.Error()), true
	}
	return m.databaseImportConnRead(abs), true
}

func (s *dbState) takenConnIDs() []string {
	var ids []string
	for _, list := range [][]dbclient.Conn{s.conns, s.userConns, s.projectConns} {
		for _, c := range list {
			ids = append(ids, c.ID)
		}
	}
	return ids
}

func (m *Model) databaseImportConnRead(path string) tea.Cmd {
	s := m.databases
	if s == nil {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return m.pushToastDetail(toast.Warn, "Cannot read "+sanitizeDBText(filepath.Base(path)), sanitizeDBText(err.Error()))
	}
	data, err := io.ReadAll(io.LimitReader(f, dbImportConnMax+1))
	f.Close()
	if err != nil {
		return m.pushToastDetail(toast.Warn, "Cannot read "+sanitizeDBText(filepath.Base(path)), sanitizeDBText(err.Error()))
	}
	if len(data) > dbImportConnMax {
		return m.pushToastDetail(toast.Warn, "Cannot import", "The file is larger than 1 MB.")
	}
	found := dbclient.ImportConnections(path, data, s.takenConnIDs())
	if len(found) == 0 {
		return m.pushToastDetail(toast.Info, "No connections found", sanitizeDBText(filepath.Base(path))+" has no connection this can read.")
	}
	s.importConns = found
	var b strings.Builder
	for _, ic := range found {
		c := ic.Conn
		where := dbConnDetail(c)
		pw := "no password"
		if ic.Secret != "" {
			pw = "password goes to the secret store"
		}
		fmt.Fprintf(&b, "%s · %s · %s\n", sanitizeDBText(c.ID), sanitizeDBText(where), pw)
	}
	b.WriteString("\nThey are added to your own connections file (not the project's).")
	label := "Import " + dbPlural(len(found), "connection")
	m.confirm = confirm.New(fmt.Sprintf("Found %s in %s", dbPlural(len(found), "connection"), sanitizeDBText(filepath.Base(path))), b.String(),
		[]confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "import", Title: label, Style: confirm.StylePrimary}}).WithScrollText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindDBImportConn
	return nil
}

// databaseImportConnConfirm saves the found connections on "import".
func (m *Model) databaseImportConnConfirm(id string) (tea.Cmd, bool) {
	if m.confirmKind != confirmKindDBImportConn {
		return nil, false
	}
	s := m.databases
	if s == nil {
		return nil, true
	}
	if id != "import" {
		s.importConns = nil
		return nil, true
	}
	needStore := false
	for _, ic := range s.importConns {
		needStore = needStore || ic.Secret != ""
	}
	if needStore {
		if store := databaseSecretStore(); store != nil && store.Locked() {
			return m.withSecretsUnlocked(func(m *Model) tea.Cmd { return m.databaseImportConnSave() }), true
		}
	}
	return m.databaseImportConnSave(), true
}

func (m *Model) databaseImportConnSave() tea.Cmd {
	s := m.databases
	found := s.importConns
	s.importConns = nil
	if len(found) == 0 {
		return nil
	}
	dir, err := configDir()
	if err != nil {
		return m.pushToastDetail(toast.Errr, "Cannot save connections", sanitizeDBText(err.Error()))
	}
	path := filepath.Join(dir, "db", "connections.json")
	all := append([]dbclient.Conn(nil), s.userConns...)
	added := make([]dbclient.Conn, 0, len(found))
	for _, ic := range found {
		c := ic.Conn
		c.Source = dbclient.SourceUser
		c.Password = ""
		if ic.Secret != "" {
			store := databaseSecretStore()
			if store == nil {
				return m.pushToastDetail(toast.Errr, "Secret store unavailable", "Nothing was imported: the passwords have nowhere safe to go.")
			}
			if err := store.Set("db."+c.ID, ic.Secret); err != nil {
				return m.pushToastDetail(toast.Errr, "Could not save password", sanitizeDBText(err.Error()))
			}
			c.Password = "${secret:db." + c.ID + "}"
		}
		all = append(all, c)
		added = append(added, c)
	}
	if err := writeDBConfig(path, all); err != nil {
		return m.pushToastDetail(toast.Errr, "Cannot save connections", sanitizeDBText(err.Error()))
	}
	s.userConns = all
	for _, c := range added {
		s.conns = append(s.conns, c)
		s.nodes[c.ID] = dbNode{kind: dbNodeConnection, name: dbConnName(c), path: c.ID, conn: c.ID, detail: dbConnDetail(c)}
	}
	s.touch()
	return m.pushToastDetail(toast.Info, "Imported "+dbPlural(len(added), "connection"), "Enter on one connects it.")
}
