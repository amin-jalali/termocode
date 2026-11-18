package git

import (
	"fmt"
	"strings"
	"testing"
	"unsafe"
)

// Memory (SCN-12): the branch name and HEAD id do not point into git's
// output. A caller that keeps only the branch (the repo scan keeps the
// name and a count) must not keep the whole output of a 50 000-change
// status alive (6-10 MB per repo).
func TestParseStatusBranchOwnsItsStrings(t *testing.T) {
	var b strings.Builder
	b.WriteString("# branch.oid 0123456789abcdef0123456789abcdef01234567\x00# branch.head main\x00")
	for i := range 1000 {
		fmt.Fprintf(&b, "? some/dir/file%04d.txt\x00", i)
	}
	out := b.String()
	bs := parseStatusV2(out)
	if bs.Branch.Name != "main" || len(bs.Files) != 1000 {
		t.Fatalf("branch %q files %d", bs.Branch.Name, len(bs.Files))
	}
	lo := uintptr(unsafe.Pointer(unsafe.StringData(out)))
	hi := lo + uintptr(len(out))
	for what, s := range map[string]string{"branch name": bs.Branch.Name, "oid": bs.OID} {
		if p := uintptr(unsafe.Pointer(unsafe.StringData(s))); p >= lo && p < hi {
			t.Errorf("the %s %q points into git's output", what, s)
		}
	}
}
