package ledger

import (
	"errors"
	"os"
	"path/filepath"
)

// ErrOwned is returned when another process owns the workspace's
// workflows (its scheduler and resume).
var ErrOwned = errors.New("another termocode owns this workspace's workflows")

// Owner is the claim of a state root: while it is held, this process
// runs the schedules and resumes interrupted runs.
type Owner struct {
	f    *os.File
	done func()
}

// ClaimOwner takes the owner lock of root (<root>/owner.lock), without
// waiting. ErrOwned: another process has it.
func ClaimOwner(root string) (*Owner, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	f, done, err := lockOwner(filepath.Join(root, "owner.lock"))
	if err != nil {
		return nil, err
	}
	return &Owner{f: f, done: done}, nil
}

// Release lets the claim go.
func (o *Owner) Release() error {
	if o == nil || o.f == nil {
		return nil
	}
	err := o.f.Close()
	o.f = nil
	if o.done != nil {
		o.done()
	}
	return err
}
