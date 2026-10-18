package secrets

// Fast scrypt in tests (the real floor is 14, the default 18).
func init() { minWorkFactor, defaultWorkFactor = 10, 10 }

// setRename swaps the rename step of atomic writes; it returns the undo.
func setRename(f func(oldpath, newpath string) error) (restore func()) {
	old := renameFn
	renameFn = f
	return func() { renameFn = old }
}
