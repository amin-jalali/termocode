package dbclient

import (
	"strings"

	"github.com/amin-jalali/termocode/internal/lazyre"
)

// Temp tables live in one server session only (DBX-40). TempTableName finds
// the temp table a statement creates, so the console can say so.

var (
	tempCreate = lazyre.New("(?is)^CREATE\\s+(?:(?:GLOBAL|LOCAL|PRIVATE)\\s+)?TEMP(?:ORARY)?\\s+TABLE\\s+(?:IF\\s+NOT\\s+EXISTS\\s+)?([\\w$.\"`\\[\\]#]+)")
	tempMSSQL  = lazyre.New("(?is)^(?:CREATE\\s+TABLE|SELECT\\b.*?\\bINTO)\\s+(#{1,2}[\\w$]+)")
)

// TempTableName is the name of the temp table sql creates ("" and false
// when it creates none). Leading comments are skipped.
func TempTableName(sql, driver string) (string, bool) {
	text := strings.TrimSpace(sql[skipLeadingComments(sql):])
	if text == "" {
		return "", false
	}
	switch normDriver(driver) {
	case "sqlserver", "mssql":
		if m := tempMSSQL.FindStringSubmatch(text); m != nil {
			return m[1], true
		}
		return "", false
	}
	if m := tempCreate.FindStringSubmatch(text); m != nil {
		return m[1], true
	}
	return "", false
}
