package ledger

import (
	"errors"
	"testing"
)

// Spec (WFX-16): one process owns a workspace's workflows (the
// scheduler and resume): a second claim of the same state root is
// refused until the first is let go, so two termocode windows on one
// folder never run a schedule twice or resume one run twice.
func TestClaimOwnerOnce(t *testing.T) {
	root := t.TempDir()
	first, err := ClaimOwner(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ClaimOwner(root); !errors.Is(err, ErrOwned) {
		t.Fatalf("second claim: %v, want ErrOwned", err)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	again, err := ClaimOwner(root)
	if err != nil {
		t.Fatalf("claim after release: %v", err)
	}
	_ = again.Release()
}
