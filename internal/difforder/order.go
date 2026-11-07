// Package difforder puts the files of a change in an order that explains
// it (CMP-58): the files that say what the change is about come first,
// then the ones that define new names, then the code that uses them, and
// the tests last. A reviewer reads the "why" before the "how".
//
// The order is a pure function of the files' names and texts. It is
// stable: files that the rules do not tell apart keep the order they
// were proposed in.
package difforder

import (
	"path"
	"regexp"
	"sort"
	"strings"
)

// File is one changed file: its workspace-relative name and both texts
// (Old is empty for a new file).
type File struct {
	Name     string
	Old, New string
}

// Class is the reading group of a file. Lower classes are read first.
type Class uint8

const (
	ClassDocs     Class = iota // notes, plans, READMEs: what and why
	ClassContract              // schemas, protos, migrations, types: the shape
	ClassCode                  // the implementation
	ClassTest                  // tests and test data: the proof
)

// Placed is one file in reading order: its index in the input and a short
// reason for its place (shown next to the file name).
type Placed struct {
	Index  int
	Class  Class
	Reason string
}

// Order returns the files in explain-first order.
func Order(files []File) []Placed {
	n := len(files)
	out := make([]Placed, n)
	defs := make([]map[string]bool, n) // names each file adds a definition for
	adds := make([]string, n)          // each file's added text
	for i, f := range files {
		out[i] = Placed{Index: i, Class: classOf(f.Name)}
		adds[i] = addedText(f.Old, f.New)
		defs[i] = newDefs(f.Old, f.New)
	}
	// uses[i][j]: file j uses a name file i defines (i should come first).
	users := make([][]int, n)
	usedBy := make([][]string, n) // the first name i defines that others use
	deps := make([]int, n)        // how many files j must wait for
	for i := range files {
		if len(defs[i]) == 0 {
			continue
		}
		names := sortedKeys(defs[i])
		for j := range files {
			if i == j || out[i].Class != out[j].Class {
				continue
			}
			for _, name := range names {
				if usesWord(adds[j], name) {
					users[i] = append(users[i], j)
					usedBy[i] = append(usedBy[i], name)
					deps[j]++
					break
				}
			}
		}
	}
	// Kahn's sort inside each class; ready files go by their input order,
	// and a cycle is broken by the earliest file left.
	byClass := map[Class][]int{}
	for i := range files {
		byClass[out[i].Class] = append(byClass[out[i].Class], i)
	}
	var order []int
	for c := ClassDocs; c <= ClassTest; c++ {
		left := append([]int(nil), byClass[c]...)
		for len(left) > 0 {
			pick := -1
			for k, i := range left {
				if deps[i] == 0 {
					pick = k
					break
				}
			}
			if pick < 0 {
				pick = 0
			}
			i := left[pick]
			left = append(left[:pick], left[pick+1:]...)
			order = append(order, i)
			deps[i] = -1
			for _, j := range users[i] {
				if deps[j] > 0 {
					deps[j]--
				}
			}
		}
	}
	res := make([]Placed, 0, n)
	for _, i := range order {
		p := out[i]
		p.Reason = reason(p.Class, usedBy[i], users[i], files)
		res = append(res, p)
	}
	return res
}

// Changed reports whether the order moves any file.
func Changed(p []Placed) bool {
	for k, x := range p {
		if x.Index != k {
			return true
		}
	}
	return false
}

func reason(c Class, names []string, users []int, files []File) string {
	switch c {
	case ClassDocs:
		return "read first: says what the change is for"
	case ClassTest:
		return "read last: tests the change"
	}
	if len(names) > 0 {
		who := path.Base(files[users[0]].Name)
		if len(users) > 1 {
			return "defines " + names[0] + ", used by " + who + " and " + plural(len(users)-1, "more file")
		}
		return "defines " + names[0] + ", used by " + who
	}
	if c == ClassContract {
		return "the shape of the change (schema, types, config)"
	}
	return ""
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return itoa(n) + " " + word + "s"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// classOf sorts a file into its reading group by its name.
func classOf(name string) Class {
	low := strings.ToLower(strings.ReplaceAll(name, "\\", "/"))
	base := path.Base(low)
	ext := path.Ext(base)
	dirs := "/" + path.Dir(low) + "/"
	switch {
	case isTest(base, dirs):
		return ClassTest
	case ext == ".md" || ext == ".mdx" || ext == ".rst" || ext == ".adoc" ||
		(ext == ".txt" && strings.Contains(dirs, "/docs/")) ||
		strings.HasPrefix(base, "readme") || strings.HasPrefix(base, "changelog"):
		return ClassDocs
	case isContract(base, ext, dirs):
		return ClassContract
	}
	return ClassCode
}

func isTest(base, dirs string) bool {
	if strings.Contains(dirs, "/testdata/") || strings.Contains(dirs, "/__tests__/") ||
		strings.Contains(dirs, "/tests/") || strings.Contains(dirs, "/test/") || strings.Contains(dirs, "/spec/") {
		return true
	}
	stem := strings.TrimSuffix(base, path.Ext(base))
	return strings.HasSuffix(stem, "_test") || strings.HasSuffix(stem, ".test") ||
		strings.HasSuffix(stem, ".spec") || strings.HasPrefix(stem, "test_") ||
		strings.HasSuffix(stem, "_spec")
}

func isContract(base, ext, dirs string) bool {
	switch ext {
	case ".proto", ".graphql", ".gql", ".sql", ".prisma", ".avsc", ".thrift":
		return true
	}
	if strings.HasSuffix(base, ".d.ts") || strings.Contains(dirs, "/migrations/") || strings.Contains(dirs, "/schema/") {
		return true
	}
	switch base {
	case "go.mod", "package.json", "cargo.toml", "pyproject.toml", "requirements.txt", "pom.xml", "build.gradle",
		"openapi.yaml", "openapi.yml", "openapi.json", "swagger.yaml", "swagger.json":
		return true
	}
	stem := strings.TrimSuffix(base, ext)
	switch stem {
	case "types", "type", "model", "models", "interface", "interfaces", "schema", "api", "contract", "proto", "dto", "entities":
		return true
	}
	return false
}

// defRE finds a definition on one line: Go, Python, JS/TS, Rust, Java,
// C# and friends (indented too: methods). topRE finds a top-level
// constant or variable (column 0 only: a local variable says nothing
// about other files). Group 1 is the defined name.
var (
	defRE = regexp.MustCompile(`^\s*(?:export\s+)?(?:default\s+)?(?:pub(?:\([a-z]+\))?\s+)?(?:public\s+|private\s+|protected\s+|internal\s+)?(?:static\s+|abstract\s+|final\s+|async\s+)*` +
		`(?:func\s+(?:\([^)]*\)\s*)?|type\s+|def\s+|class\s+|interface\s+|struct\s+|enum\s+|trait\s+|fn\s+|function\s+|record\s+)` +
		`([A-Za-z_][A-Za-z0-9_]*)`)
	topRE = regexp.MustCompile(`^(?:export\s+)?(?:pub\s+)?(?:const|var|let|static)\s+([A-Za-z_][A-Za-z0-9_]*)`)
)

// defName is the name a line defines ("" when none).
func defName(l string) string {
	if m := defRE.FindStringSubmatch(l); m != nil {
		return m[1]
	}
	if m := topRE.FindStringSubmatch(l); m != nil {
		return m[1]
	}
	return ""
}

// newDefs returns the names New defines that Old did not.
func newDefs(old, new string) map[string]bool {
	had := map[string]bool{}
	for _, l := range strings.Split(old, "\n") {
		if d := defName(l); d != "" {
			had[d] = true
		}
	}
	out := map[string]bool{}
	for _, l := range strings.Split(new, "\n") {
		if d := defName(l); d != "" && !had[d] && len(d) >= 3 && !common[d] {
			out[d] = true
		}
	}
	return out
}

// common names say nothing about who uses whom.
var common = map[string]bool{"main": true, "init": true, "err": true, "new": true, "New": true, "self": true, "this": true,
	"ctx": true, "res": true, "out": true, "tmp": true, "val": true, "key": true, "msg": true, "cmd": true}

// addedText is the lines of New that are not lines of Old (as a set: a
// cheap stand-in for the diff's added side).
func addedText(old, new string) string {
	had := map[string]int{}
	for _, l := range strings.Split(old, "\n") {
		had[l]++
	}
	var b strings.Builder
	for _, l := range strings.Split(new, "\n") {
		if had[l] > 0 {
			had[l]--
			continue
		}
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return b.String()
}

// usesWord reports whether text holds name as a whole word.
func usesWord(text, name string) bool {
	for off := 0; ; {
		k := strings.Index(text[off:], name)
		if k < 0 {
			return false
		}
		s, e := off+k, off+k+len(name)
		if (s == 0 || !wordByte(text[s-1])) && (e == len(text) || !wordByte(text[e])) {
			return true
		}
		off = e
	}
}

func wordByte(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
