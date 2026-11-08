package dbclient

import "context"

// Session is one database connection. Implementations must enforce read-only
// mode at the server or file-system boundary, not through Classify alone.
type Session interface {
	Ping(context.Context) error
	Introspect(context.Context, IntrospectionRequest) (IntrospectionResult, error)
	Run(context.Context, string, RunOptions) Result
	Begin(context.Context) error
	Commit(context.Context) error
	Rollback(context.Context) error
	ReadOnlySession() (Session, error)
	Cancel()
	Close() error
}

type RunOptions struct {
	MaxRows int
	// Args are bound to the statement's placeholders ($1 or ?).
	Args []any
}

type IntrospectionRequest struct {
	Database string
	Schema   string
	Object   string
	Kind     string
	// Lean asks only for what a list needs, each catalog query being a
	// round trip or more: a schema's objects (no routines, sequences,
	// triggers …), or an object's key (its primary key, else its indexes).
	// A session may answer more.
	Lean bool
}

type IntrospectionResult struct {
	Databases   []string
	Schemas     []string
	Objects     []SchemaObject
	Columns     []Column
	Indexes     []SchemaIndex
	ForeignKeys []SchemaForeignKey
	DDL         string
	Keys        []string
	Routines    []string
	Sequences   []string
	// Packages are Oracle PL/SQL packages (DBX-63), schema level and
	// optional.
	Packages []string
	// Triggers and Extensions are schema level, Checks object level
	// (DBX-13). They are optional: a server that cannot list them
	// leaves them empty.
	Triggers   []string
	Extensions []string
	Checks     []SchemaCheck
	// Partitions (object level) and Roles (connection level) are
	// optional too (DBX-13).
	Partitions []string
	Roles      []string
}

// SchemaCheck is one check constraint of a table.
type SchemaCheck struct {
	Name, Expr string
}

type SchemaObject struct {
	Name string
	Kind string
}

type SchemaIndex struct {
	Name    string
	Columns []string
	Unique  bool
}

type SchemaForeignKey struct {
	Name, Column, RefTable, RefColumn string
	// Virtual marks a key from the user's virtual objects, not the database.
	Virtual bool
}
