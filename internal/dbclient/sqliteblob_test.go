package dbclient

import (
	"fmt"
	"regexp"
	"testing"
)

// The regexps the byte scans replace: they stay the reference.
var (
	refRawByteEscape  = regexp.MustCompile(`(?i)\\u00[89a-f][0-9a-f]`)
	refJSONBlobEscape = regexp.MustCompile(`(?i)u[0-9a-f]{8}|u00[01][0-9a-f]`)
)

var blobEscapeCases = []string{
	"", "u", "plain text", `\u0080`, `\U00Ff`, `\u007f`, `\u00g0`, `x\u00a`, `© copyright`,
	"u0000", "U001f", "u0020", "u002", "uFFFFFFFF", "uffffff9c", "uFFFFFFF", "uFFFFFFFg",
	"user 00000001", "u00010000", "menu0001x", `{"a":"é"}`, "uu0001", `\\u0080`,
}

func checkBlobScans(t *testing.T, s string) {
	t.Helper()
	if got, want := sqliteRawByteEscape(s), refRawByteEscape.MatchString(s); got != want {
		t.Fatalf("sqliteRawByteEscape(%q) = %v, regexp %v", s, got, want)
	}
	if got, want := sqliteJSONBlobEscape(s), refJSONBlobEscape.MatchString(s); got != want {
		t.Fatalf("sqliteJSONBlobEscape(%q) = %v, regexp %v", s, got, want)
	}
}

func TestSQLiteBlobScansMatchRegexps(t *testing.T) {
	for _, s := range blobEscapeCases {
		checkBlobScans(t, s)
	}
}

func FuzzSQLiteBlobScans(f *testing.F) {
	for _, s := range blobEscapeCases {
		f.Add(s)
	}
	f.Fuzz(checkBlobScans)
}

func BenchmarkSQLiteFindBlob(b *testing.B) {
	lines := sqliteJSONRows(10000)
	rows, _, err := parseSQLiteJSON(lines)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, ok := sqliteFindBlob(lines, rows); ok {
			b.Fatal(fmt.Sprint("no blob in ", len(rows), " rows"))
		}
	}
}
