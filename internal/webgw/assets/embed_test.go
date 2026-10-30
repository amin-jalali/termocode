package assets

import (
	"io/fs"
	"testing"

	"github.com/amin-jalali/termocode/internal/webgw/assets/manifest"
)

func TestEmbeddedFilesMatchTheLock(t *testing.T) {
	entries := Lock()
	if len(entries) == 0 {
		t.Fatal("empty lock")
	}
	for _, e := range entries {
		b, err := fs.ReadFile(FS(), e.Path)
		if err != nil {
			t.Errorf("%s is in the lock but not embedded: %v", e.Path, err)
			continue
		}
		if manifest.SRI(b) != e.SRI || len(b) != e.Size {
			t.Errorf("embedded %s does not match its lock row", e.Path)
		}
	}
}

func TestEmbedHoldsNoSourcesOrVendorFiles(t *testing.T) {
	for _, p := range []string{"src", "vendor", "manifest", "doc.go"} {
		if _, err := fs.Stat(FS(), p); err == nil {
			t.Errorf("%s is embedded; only built files may be", p)
		}
	}
}
