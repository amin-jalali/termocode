package webproto

import (
	"strings"
	"testing"
)

// Spec (DBX-67): CQL has no OFFSET, so db.rows and db.query take a cursor,
// the CQL page state in base64, that the previous page handed back. A
// cursor that is not base64, or that is too long, is refused.
func TestDBCursorArgs(t *testing.T) {
	bad := []string{`!!`, `abc`, strings.Repeat("A", MaxDBCursor+4)}
	var rowsBad, rowsOK, queryBad, queryOK []string
	for _, c := range bad {
		rowsBad = append(rowsBad, `{"conn":"wide","table":"shop.orders","cursor":"`+c+`"}`)
		queryBad = append(queryBad, `{"conn":"wide","sql":"SELECT 1","cursor":"`+c+`"}`)
	}
	rowsOK = append(rowsOK, `{"conn":"wide","table":"shop.orders","cursor":"AQID"}`)
	queryOK = append(queryOK, `{"conn":"wide","sql":"SELECT 1","cursor":"AQID"}`, `{"conn":"wide","sql":"SELECT 1","cursor":""}`)
	checkArgs(t, OpDBRows, rowsBad, rowsOK)
	checkArgs(t, OpDBQuery, queryBad, queryOK)
}
