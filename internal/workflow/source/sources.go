package source

// Sources runs source steps for one workspace. The zero value of each
// optional field is safe; an adapter whose dependency is nil fails with
// a clear error.
type Sources struct {
	// Workspace is the root every path (shell cwd, files glob, .http
	// request file) must stay inside.
	Workspace string
}
