package dbclient

import (
	"fmt"
	"math/rand/v2"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Test data generator (DBX-20): INSERT statements with made-up rows for
// one table, shaped by the column types and names, with foreign key
// columns filled from values that exist in the referenced table. It only
// writes text; the user reviews the script and runs it like any other.

// GenColumn is one column of the target table.
type GenColumn struct {
	Name    string
	Type    string // as the catalog says: "integer", "varchar(40)", "enum('a','b')"
	NotNull bool
	PK      bool
	Default string // the column default ("" = none)
}

// GenOptions says how many rows to make and what the foreign keys may
// point at.
type GenOptions struct {
	Rows int
	Seed uint64
	// FKValues: per column name, existing values of the referenced
	// column, as text (they are quoted as literals). A foreign key
	// column without values gets NULL (or is noted when NOT NULL).
	FKValues map[string][]string
	// FKColumns marks the foreign key columns (also those without
	// values).
	FKColumns map[string]bool
}

// GenMaxRows is the most rows one script may hold.
const GenMaxRows = 10000

const genBatch = 100

// GenerateTestData writes the INSERT script for table (already quoted:
// "public"."users") in dialect d. The first lines are -- comments: what
// was made and which columns were left to the database.
func GenerateTestData(d Dialect, table string, cols []GenColumn, opt GenOptions) string {
	n := min(max(opt.Rows, 1), GenMaxRows)
	r := rand.New(rand.NewPCG(opt.Seed, opt.Seed^0x9e3779b97f4a7c15))
	var b strings.Builder
	fmt.Fprintf(&b, "-- Test data for %s: %d rows (made up; review before you run it)\n", table, n)
	pks := 0
	for _, c := range cols {
		if c.PK {
			pks++
		}
	}
	var use []GenColumn
	for _, c := range cols {
		if genAuto(c, pks) {
			fmt.Fprintf(&b, "-- %s is left to the database (auto number or default)\n", c.Name)
			continue
		}
		if opt.FKColumns[c.Name] && len(opt.FKValues[c.Name]) == 0 {
			if c.NotNull {
				fmt.Fprintf(&b, "-- %s: the referenced table has no rows; add some there first\n", c.Name)
			} else {
				fmt.Fprintf(&b, "-- %s: the referenced table has no rows; NULL is used\n", c.Name)
			}
		}
		use = append(use, c)
	}
	if len(use) == 0 {
		b.WriteString("-- every column is filled by the database\n")
		for i := 0; i < n; i += genBatch {
			for j := i; j < min(i+genBatch, n); j++ {
				if _, my := d.(MySQLDialect); my {
					fmt.Fprintf(&b, "INSERT INTO %s () VALUES ();\n", table)
				} else {
					fmt.Fprintf(&b, "INSERT INTO %s DEFAULT VALUES;\n", table)
				}
			}
		}
		return b.String()
	}
	names := make([]string, len(use))
	for i, c := range use {
		names[i] = d.QuoteIdent(c.Name)
	}
	head := "INSERT INTO " + table + " (" + strings.Join(names, ", ") + ") VALUES\n"
	for i := 0; i < n; i += genBatch {
		b.WriteString(head)
		end := min(i+genBatch, n)
		for row := i; row < end; row++ {
			vals := make([]string, len(use))
			for k, c := range use {
				vals[k] = genValue(d, c, row, r, opt)
			}
			b.WriteString("  (" + strings.Join(vals, ", ") + ")")
			if row+1 < end {
				b.WriteString(",\n")
			} else {
				b.WriteString(";\n")
			}
		}
	}
	return b.String()
}

// genAuto: the database fills this column (sequence / identity default,
// or a lone integer primary key: serial, AUTO_INCREMENT, SQLite rowid).
func genAuto(c GenColumn, pks int) bool {
	def := strings.ToLower(c.Default)
	if strings.Contains(def, "nextval(") || strings.Contains(def, "identity") || strings.Contains(def, "auto_increment") {
		return true
	}
	return c.PK && pks == 1 && genIsInt(strings.ToLower(c.Type))
}

func genIsInt(t string) bool {
	return strings.Contains(t, "int") || strings.Contains(t, "serial")
}

var (
	genEnumRe = regexp.MustCompile(`^(?:enum|set)\((.*)\)$`)
	genLenRe  = regexp.MustCompile(`\((\d+)\)`)
)

var (
	genFirst  = []string{"Ada", "Ben", "Chloe", "Dan", "Eva", "Finn", "Grace", "Hugo", "Iris", "Jon", "Kara", "Leo", "Maya", "Nora", "Omar", "Pia"}
	genLast   = []string{"Stone", "Rivers", "Hill", "Brook", "Field", "Wood", "Lake", "Marsh", "Cliff", "Dale"}
	genCities = []string{"Lisbon", "Oslo", "Kyoto", "Lima", "Accra", "Perth", "Quebec", "Tartu"}
	genWords  = []string{"amber", "brisk", "cedar", "delta", "ember", "fable", "grove", "harbor", "ivory", "jade", "kite", "lumen", "maple", "nova", "orbit", "pine"}
)

func genValue(d Dialect, c GenColumn, row int, r *rand.Rand, opt GenOptions) string {
	if opt.FKColumns[c.Name] {
		vs := opt.FKValues[c.Name]
		if len(vs) == 0 {
			return "NULL"
		}
		return genQuote(d, vs[r.IntN(len(vs))])
	}
	t := strings.ToLower(strings.TrimSpace(c.Type))
	name := strings.ToLower(c.Name)
	_, pg := d.(PostgresDialect)
	_, my := d.(MySQLDialect)
	switch {
	case genEnumRe.MatchString(t):
		if opts := genEnumValues(c.Type); len(opts) > 0 {
			return genQuote(d, opts[r.IntN(len(opts))])
		}
	case strings.Contains(t, "bool") || (my && strings.HasPrefix(t, "tinyint(1)")):
		v := r.IntN(2) == 1
		if pg {
			return strconv.FormatBool(v)
		}
		if v {
			return "1"
		}
		return "0"
	case genIsInt(t) && !strings.Contains(t, "interval") && !strings.Contains(t, "point"):
		if c.PK {
			return strconv.Itoa(row + 1)
		}
		top := 1000
		if strings.Contains(t, "tinyint") {
			top = 100
		}
		return strconv.Itoa(1 + r.IntN(top))
	case strings.Contains(t, "numeric") || strings.Contains(t, "decimal") || strings.Contains(t, "real") ||
		strings.Contains(t, "double") || strings.Contains(t, "float") || strings.Contains(t, "money"):
		return fmt.Sprintf("%d.%02d", r.IntN(1000), r.IntN(100))
	case strings.Contains(t, "uuid"):
		return genQuote(d, genUUID(r))
	case strings.Contains(t, "json"):
		return genQuote(d, fmt.Sprintf(`{"n": %d, "tag": %q}`, row+1, genWords[r.IntN(len(genWords))]))
	case strings.Contains(t, "timestamp") || strings.Contains(t, "datetime"):
		return genQuote(d, genTime(r).Format("2006-01-02 15:04:05"))
	case t == "date":
		return genQuote(d, genTime(r).Format("2006-01-02"))
	case strings.HasPrefix(t, "time"):
		return genQuote(d, genTime(r).Format("15:04:05"))
	case t == "year":
		return strconv.Itoa(2000 + r.IntN(26))
	case strings.Contains(t, "bytea"):
		return "'\\x" + fmt.Sprintf("%08x", r.Uint32()) + "'"
	case strings.Contains(t, "blob") || strings.Contains(t, "binary"):
		return "X'" + fmt.Sprintf("%08x", r.Uint32()) + "'"
	}
	s := genText(name, row, r)
	if m := genLenRe.FindStringSubmatch(t); m != nil && (strings.Contains(t, "char") || strings.Contains(t, "text")) {
		if n, _ := strconv.Atoi(m[1]); n > 0 && len(s) > n {
			s = s[:n]
		}
	}
	return genQuote(d, s)
}

// genText picks a value by the column name; row keeps it unique.
func genText(name string, row int, r *rand.Rand) string {
	first, last := genFirst[r.IntN(len(genFirst))], genLast[r.IntN(len(genLast))]
	switch {
	case strings.Contains(name, "email") || strings.Contains(name, "mail"):
		return fmt.Sprintf("%s.%s%d@example.test", strings.ToLower(first), strings.ToLower(last), row+1)
	case name == "first_name" || name == "firstname" || name == "given_name":
		return first
	case name == "last_name" || name == "lastname" || name == "surname" || name == "family_name":
		return last
	case strings.Contains(name, "user") && strings.Contains(name, "name"), name == "login", name == "handle":
		return fmt.Sprintf("%s%d", strings.ToLower(first), row+1)
	case strings.Contains(name, "name"):
		return first + " " + last
	case strings.Contains(name, "phone") || strings.Contains(name, "mobile"):
		return fmt.Sprintf("+1-555-%04d", r.IntN(10000))
	case strings.Contains(name, "url") || strings.Contains(name, "website") || strings.Contains(name, "link"):
		return fmt.Sprintf("https://example.test/%s/%d", genWords[r.IntN(len(genWords))], row+1)
	case strings.Contains(name, "city"):
		return genCities[r.IntN(len(genCities))]
	case strings.Contains(name, "country"):
		return []string{"PT", "NO", "JP", "PE", "GH", "AU", "CA", "EE"}[r.IntN(8)]
	case strings.Contains(name, "status") || strings.Contains(name, "state"):
		return []string{"active", "pending", "closed"}[r.IntN(3)]
	case strings.Contains(name, "code") || strings.Contains(name, "sku"):
		return fmt.Sprintf("%s-%04d", strings.ToUpper(genWords[r.IntN(len(genWords))][:3]), row+1)
	}
	return fmt.Sprintf("%s %s %d", genWords[r.IntN(len(genWords))], genWords[r.IntN(len(genWords))], row+1)
}

func genEnumValues(typ string) []string {
	m := genEnumRe.FindStringSubmatch(strings.TrimSpace(typ))
	if m == nil {
		return nil
	}
	var out []string
	for _, p := range strings.Split(m[1], ",") {
		p = strings.TrimSpace(p)
		if len(p) >= 2 && p[0] == '\'' && p[len(p)-1] == '\'' {
			out = append(out, strings.ReplaceAll(p[1:len(p)-1], "''", "'"))
		}
	}
	return out
}

var genEpoch = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

func genTime(r *rand.Rand) time.Time {
	return genEpoch.Add(time.Duration(r.IntN(2*365*24*3600)) * time.Second)
}

func genUUID(r *rand.Rand) string {
	a, b := r.Uint64(), r.Uint64()
	a = a&^0xf000 | 0x4000     // version 4
	b = b&^(0xc<<60) | 0x8<<60 // variant 10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", a>>32, (a>>16)&0xffff, a&0xffff, b>>48, b&0xffffffffffff)
}

// genQuote is a string literal; MySQL also reads backslashes as escapes.
func genQuote(d Dialect, s string) string {
	if _, my := d.(MySQLDialect); my {
		s = strings.ReplaceAll(s, `\`, `\\`)
	}
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
