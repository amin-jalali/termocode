// Package layout is the file format and the store of named layouts: which
// panels are open, how big they are, the editor splits, the terminal tabs
// and (optionally) the open files. A layout is a small JSONC file the user
// can save, open, switch, and share.
//
// The package is pure: it reads and writes files and builds text. The app
// package captures the live layout and applies a loaded one.
//
// A layout never holds secrets, environment values or terminal output:
// there is simply no field for them, and unknown keys are dropped when a
// layout is saved again.
package layout
