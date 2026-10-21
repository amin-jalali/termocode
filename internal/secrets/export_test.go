package secrets

// Fast scrypt in tests (the real floor is 14, the default 18).
func init() { minWorkFactor, defaultWorkFactor = 10, 10 }

// setRename swaps the rename step of atomic writes; it returns the undo.
func setRename(f func(oldpath, newpath string) error) (restore func()) {
	old := renameFn
	renameFn = f
	return func() { renameFn = old }
}

// setRemove swaps the delete step of Remove; it returns the undo.
func setRemove(f func(path string) error) (restore func()) {
	old := removeFn
	removeFn = f
	return func() { removeFn = old }
}

// setVerify swaps the read-back check of Migrate; it returns the undo.
func setVerify(f func(Store, map[string]string) error) (restore func()) {
	old := verifyFn
	verifyFn = f
	return func() { verifyFn = old }
}
