package app

// PanelKey is one row of the table of keys that Databases and Containers
// share (KB-3). A key has one role; each panel says what it does there.
// An empty cell means the panel does not use the key for that role.
type PanelKey struct {
	Key        string
	Role       string
	Databases  string
	Containers string
}

// PanelKeyCatalog is the one table behind both `?` cards and the keys
// page of the reference. A test keeps the cards and the table in step.
func PanelKeyCatalog() []PanelKey {
	return []PanelKey{
		{"a", "add", "add a connection", ""},
		{"D", "remove (always asks first)", "drop the table, view, schema or database", "remove a container, image, network, volume or context; project down; sign out"},
		{"s", "start", "", "start a container or a project; on the SERVICES header, start all"},
		{"x", "stop", "disconnect", "stop a container or a project; on the SERVICES header, stop all"},
		{"$", "shell in a terminal", "the connection's own CLI (psql, mysql…)", "shell in a container or service"},
		{"M", "mode", "transaction mode of a connection", ""},
		{"Q", "query", "select 100 rows of the table in the console", ""},
		{"v", "view the definition", "show the DDL", ""},
		{"U", "usage", "run pgTAP tests (PostgreSQL)", "read volume disk usage"},
		{"f", "find", "", "search Docker Hub (REGISTRIES)"},
		{"u", "use or update", "", "pull an image; make a context the default"},
		{"y", "copy", "copy the name", "copy the id"},
		{"R", "refresh", "refresh the tree", "refresh the rows"},
	}
}
