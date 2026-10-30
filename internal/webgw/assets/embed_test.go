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
		src := FS()
		if IsKitFile(e.Path) {
			if !KitEnabled {
				if _, err := fs.Stat(FS(), e.Path); err == nil {
					t.Errorf("%s is embedded in a release build", e.Path)
				}
				continue
			}
			src = KitFS()
		}
		b, err := fs.ReadFile(src, e.Path)
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
