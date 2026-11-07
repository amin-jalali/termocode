package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// The connection options form (DBX-28): a picker of every option with
// its value; picking one asks for the new value, which is checked and
// saved to your connections file, then the picker opens again.

func dbConnOpts(c dbclient.Conn) dbclient.ConnOptions {
	if c.Options == nil {
		return dbclient.ConnOptions{}
	}
	return *c.Options
}

// dbOptionItems are the picker rows: title, then the value or the hint.
func dbOptionItems(o dbclient.ConnOptions) []picker.Item {
	items := make([]picker.Item, 0, len(dbclient.ConnOptionFields))
	for _, f := range dbclient.ConnOptionFields {
		hint := "not set · " + f.Hint
		if v := o.Field(f.ID); v != "" {
			hint = sanitizeDBText(oneLineCut(v, 60))
		}
		items = append(items, picker.Item{ID: f.ID, Title: f.Title, Hint: hint})
	}
	return items
}

func oneLineCut(s string, n int) string {
	r := []rune(s)
	for i, c := range r {
		if c == '\n' || c == '\r' {
			r = append(r[:i:i], '…')
			break
		}
	}
	if len(r) > n {
		r = append(r[:n-1:n-1], '…')
	}
	return string(r)
}

func (m *Model) dbOpenOptions(c dbclient.Conn) {
	m.dbOpenPicker(pickerKindDBOptions, " Options · "+sanitizeDBText(dbConnName(c))+" ", dbOptionItems(dbConnOpts(c)))
}

func dbOptionField(id string) (dbclient.ConnOptionField, bool) {
	for _, f := range dbclient.ConnOptionFields {
		if f.ID == id {
			return f, true
		}
	}
	return dbclient.ConnOptionField{}, false
}

// dbOptionAsk opens the prompt for one option with its saved value.
func (m *Model) dbOptionAsk(id string) tea.Cmd {
	c, ok := m.dbManaged()
	if !ok {
		return nil
	}
	return m.dbOptionPrompt(id, dbConnOpts(c).Field(id))
}

func (m *Model) dbOptionPrompt(id, value string) tea.Cmd {
	c, ok := m.dbManaged()
	f, known := dbOptionField(id)
	if !ok || !known {
		return nil
	}
	m.databases.manage.optField = id
	return m.dbManagePrompt(promptKindDBOptionField, f.Title+" · "+sanitizeDBText(dbConnName(c)),
		f.Hint+" (empty clears it):", value)
}

// dbApplyOption checks and saves the typed value. A bad value keeps the
// prompt open with the reason; nothing is saved.
func (m *Model) dbApplyOption(value string) tea.Cmd {
	c, ok := m.dbManaged()
	if !ok {
		return nil
	}
	ms := m.databases.manage
	o := dbConnOpts(c)
	o.Databases = append([]string(nil), o.Databases...)
	o.Schemas = append([]string(nil), o.Schemas...)
	o.Objects = append([]string(nil), o.Objects...)
	o.ReadOnlyTables = append([]string(nil), o.ReadOnlyTables...)
	err := o.SetField(ms.optField, value)
	var users []dbclient.Conn
	if err == nil {
		users, err = dbclient.SetConnOptions(m.databases.userConns, c.ID, o)
	}
	if err != nil {
		cmd := m.dbOptionPrompt(ms.optField, value)
		m.prompt.SetError(sanitizeDBText(err.Error()))
		return cmd
	}
	if err := m.dbSaveUserConns(users); err != nil {
		return m.dbManageFail("Cannot save the connection", err)
	}
	if c2, ok := m.dbManaged(); ok {
		m.dbOpenOptions(c2)
	}
	note := ""
	if _, open := m.databases.sessions[c.ID]; open {
		note = "Connect again to use the new options."
	}
	return m.pushToastDetail(toast.Info, "Option saved", note)
}
