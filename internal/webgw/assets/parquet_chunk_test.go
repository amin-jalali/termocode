package assets

import (
	"io/fs"
	"strings"
	"testing"
)

// budgetParquet is Export as Parquet of the desktop's database grid
// (parquet.js), gzipped; the workbench loads it when the entry is used.
const budgetParquet = 2 << 10

// The Parquet export is in parquet.js alone: the workbench (whose budget is
// full) holds only the menu entry and the one dynamic import (lib/parquet.ts),
// and parquet.js imports nothing: the workbench gives it the link and the
// download.
func TestParquetChunkHoldsTheExport(t *testing.T) {
	if _, err := fs.Stat(FS(), "parquet.js"); err != nil {
		t.Fatalf("no parquet.js: %v", err)
	}
	pq, wb, page := readBuilt(t, "parquet.js"), readBuilt(t, "workbench.js"), readPage(t)
	if !strings.Contains(pq, `"db.export_parquet"`) {
		t.Error("parquet.js does not call db.export_parquet")
	}
	if strings.Contains(wb, `"db.export_parquet"`) || strings.Contains(page, `"db.export_parquet"`) {
		t.Error("the workbench or the phone's page holds the op: it belongs in parquet.js")
	}
	for _, bad := range []string{"import ", "import(", "app-chunk"} {
		if strings.Contains(pq, bad) {
			t.Errorf("parquet.js imports something (%q): the workbench gives it what it needs", bad)
		}
	}
	if !strings.Contains(wb, `import("/assets/parquet.js")`) {
		t.Error("the workbench does not load /assets/parquet.js")
	}
	if !strings.Contains(readBuilt(t, "aictx.js"), `"Parquet"`) { // the export menu lives in aictx.js (aictx/main.tsx)
		t.Error("the export menu has no Parquet entry")
	}
	if strings.Contains(readBuilt(t, "index.html"), "parquet.js") {
		t.Error("index.html names parquet.js: only the workbench loads it, on use")
	}
	if n := gzSize(t, "parquet.js"); n > budgetParquet {
		t.Errorf("parquet.js is %d B gzipped, budget %d B", n, budgetParquet)
	} else {
		t.Logf("gzipped: parquet.js %d B (desktop only, on first use)", n)
	}
}
