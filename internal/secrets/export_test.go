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

// setReencryptCheck swaps the check of a re-encrypted file before it
// replaces the old one; it returns the undo.
func setReencryptCheck(f func(tmp string, pass []byte, want map[string]string) error) (restore func()) {
	old := reencryptCheckFn
	reencryptCheckFn = f
	return func() { reencryptCheckFn = old }
}

// setSwitchVerify swaps the read-back check of Switch; it returns the undo.
func setSwitchVerify(f func(Backend, map[string]string) error) (restore func()) {
	old := switchVerifyFn
	switchVerifyFn = f
	return func() { switchVerifyFn = old }
}
