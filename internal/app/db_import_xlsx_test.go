package app

import "testing"

// DBX-37: an .xlsx file is converted to CSV first, like JSON.
func TestDBImportKnowsExcelFiles(t *testing.T) {
	for _, n := range []string{"a.xlsx", "B.XLSX"} {
		if !dbImportIsXLSX(n) {
			t.Errorf("%s is not seen as Excel", n)
		}
		if !dbImportNeedsConvert(n) {
			t.Errorf("%s would not be converted", n)
		}
	}
	for _, n := range []string{"a.csv", "a.xls", "a.xlsx.csv"} {
		if dbImportIsXLSX(n) {
			t.Errorf("%s is seen as Excel", n)
		}
	}
	if !dbImportNeedsConvert("a.json") || dbImportNeedsConvert("a.csv") {
		t.Error("JSON converts, CSV does not")
	}
}
