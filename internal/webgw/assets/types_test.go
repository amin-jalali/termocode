package assets

import (
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Type safety without node. esbuild strips types and never type checks,
// and CI has no node and no tsc (owner rule). These tests close the gaps
// that cost the most and can be checked by reading the sources:
//
//   - every named import, value or type, names an export of the file it
//     imports (esbuild catches a missing value import from a bundled
//     module, but drops type imports unread);
//   - imports from the vendored libraries name real exports of the
//     vendored JS; type-only names from them are checked against a fixed
//     list, because their .d.ts files are not vendored;
//   - every field the schema guard (src/proto/guard.ts) reads exists, with
//     that JSON name, on the webproto Go type of that message.
//
// What stays unchecked (honest limit): types of expressions, function
// arguments and return values, JSX props, null checks, and the shapes of
// topic data. A wrong prop or a wrong argument type builds and runs; the
// browser tests in e2e/web catch what they exercise.

var (
	importRe     = regexp.MustCompile(`(?s)\b(import|export)\s+(type\s+)?([^;"']*?)\s*\bfrom\s*["']([^"']+)["']`)
	exportRe     = regexp.MustCompile(`(?m)^export\s+(?:declare\s+)?(?:default\s+)?(?:async\s+)?(?:abstract\s+)?(?:function\*?|const|let|var|class|interface|type|enum)\s+([A-Za-z_$][\w$]*)`)
	exportListRe = regexp.MustCompile(`(?s)\bexport\s*(?:type\s+)?\{([^}]*)\}`)
)

// importedName is one name an import statement asks for.
type importedName struct {
	name   string
	isType bool
}

// parseImportClause reads `X, { a, type b, c as d }` (or `* as ns`, or
// nothing for `export * from`).
func parseImportClause(clause string, allType bool) (names []importedName, star bool) {
	clause = strings.TrimSpace(clause)
	if strings.HasPrefix(clause, "*") {
		return nil, true
	}
	open, close := strings.Index(clause, "{"), strings.LastIndex(clause, "}")
	if open < 0 || close < open {
		return nil, false // a default import only; our sources have none
	}
	for _, item := range strings.Split(clause[open+1:close], ",") {
		f := strings.Fields(item)
		if len(f) == 0 {
			continue
		}
		isType := allType
		if f[0] == "type" && len(f) > 1 {
			isType, f = true, f[1:]
		}
		names = append(names, importedName{name: f[0], isType: isType})
	}
	return names, false
}

// tsExports lists the names a TS source exports.
func tsExports(src string) map[string]bool {
	src = regexp.MustCompile(`(?m)//.*$|(?s)/\*.*?\*/`).ReplaceAllString(src, "")
	out := map[string]bool{}
	for _, m := range exportRe.FindAllStringSubmatch(src, -1) {
		out[m[1]] = true
	}
	for _, m := range exportListRe.FindAllStringSubmatch(src, -1) {
		for _, item := range strings.Split(m[1], ",") {
			f := strings.Fields(item)
			if len(f) == 0 {
				continue
			}
			out[f[len(f)-1]] = true // "a as b" exports b
		}
	}
	return out
}

// jsExports lists the names a vendored, minified ES module exports, and
// the bare modules it re-exports from.
func jsExports(src string) (names map[string]bool, from []string) {
	names = map[string]bool{}
	for _, m := range regexp.MustCompile(`export\s*\{([^}]*)\}\s*(?:from\s*["']([^"']+)["'])?`).FindAllStringSubmatch(src, -1) {
		for _, item := range strings.Split(m[1], ",") {
			f := strings.Fields(item)
			if len(f) > 0 {
				names[f[len(f)-1]] = true
			}
		}
	}
	for _, m := range regexp.MustCompile(`export\s*\*\s*from\s*["']([^"']+)["']`).FindAllStringSubmatch(src, -1) {
		from = append(from, m[1])
	}
	return names, from
}

// vendorTypes are the type-only names our sources take from the vendored
// libraries. This list is a cheap check that runs without tsc; the vendored
// .d.ts files and TestTypeScriptTypeChecks are the real one.
var vendorTypes = map[string]map[string]bool{
	"preact":           {"VNode": true, "ComponentChildren": true, "ComponentType": true, "FunctionComponent": true, "JSX": true, "Ref": true},
	"preact/hooks":     {"MutableRef": true},
	"@preact/signals":  {"ReadonlySignal": true, "Signal": true},
	"@codemirror/view": {"DecorationSet": true},
}

// resolveTS finds the file a relative import names.
func resolveTS(files map[string]string, from, spec string) (string, bool) {
	base := path.Join(path.Dir(from), spec)
	for _, ext := range []string{"", ".ts", ".tsx", ".d.ts", "/index.ts"} {
		if _, ok := files[base+ext]; ok {
			return base + ext, true
		}
	}
	return "", false
}

// checkImports returns every import that names something its target
// does not export. files maps slash paths to sources; vendor maps a bare
// specifier to its JS source.
func checkImports(files map[string]string, vendor map[string]string) []string {
	var bad []string
	exports := map[string]map[string]bool{}
	for p, src := range files {
		exports[p] = tsExports(src)
	}
	vendorExports := func(spec string) map[string]bool {
		out := map[string]bool{}
		seen := map[string]bool{}
		var walk func(string)
		walk = func(s string) {
			if seen[s] {
				return
			}
			seen[s] = true
			names, from := jsExports(vendor[s])
			for n := range names {
				out[n] = true
			}
			for _, f := range from {
				walk(f)
			}
		}
		walk(spec)
		return out
	}
	for p, src := range files {
		if !strings.HasSuffix(p, ".ts") && !strings.HasSuffix(p, ".tsx") || strings.HasSuffix(p, ".d.ts") {
			continue
		}
		code := regexp.MustCompile(`(?m)//.*$|(?s)/\*.*?\*/`).ReplaceAllString(src, "")
		for _, m := range importRe.FindAllStringSubmatch(code, -1) {
			names, _ := parseImportClause(m[3], m[2] != "")
			spec := m[4]
			if strings.HasPrefix(spec, ".") {
				target, ok := resolveTS(files, p, spec)
				if !ok {
					bad = append(bad, p+": "+spec+" names no file")
					continue
				}
				for _, n := range names {
					if !exports[target][n.name] {
						bad = append(bad, p+": "+target+" does not export "+n.name)
					}
				}
				continue
			}
			if _, ok := vendor[spec]; !ok {
				bad = append(bad, p+": "+spec+" is not a vendored module")
				continue
			}
			have := vendorExports(spec)
			for _, n := range names {
				if have[n.name] || (n.isType && vendorTypes[spec][n.name]) {
					continue
				}
				bad = append(bad, p+": "+spec+" does not export "+n.name)
			}
		}
	}
	sort.Strings(bad)
	return bad
}

// loadSources reads src/ and the generated protocol types, keyed as the
// imports see them (webproto's proto.d.ts sits four levels up from src/net).
func loadSources(t *testing.T) (map[string]string, map[string]string) {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir("src", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if e := filepath.Ext(p); e != ".ts" && e != ".tsx" {
			return nil
		}
		b, err := os.ReadFile(p)
		files[filepath.ToSlash(p)] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	proto, err := os.ReadFile("../../webproto/proto.d.ts")
	if err != nil {
		t.Fatal(err)
	}
	// src/x/y.ts imports "../../../../webproto/proto": that is
	// internal/webproto/proto.d.ts seen from internal/webgw/assets/src.
	files[path.Join("src", "../../../webproto/proto.d.ts")] = string(proto)

	var lock struct {
		Packages []struct {
			Dir     string            `json:"dir"`
			Imports map[string]string `json:"imports"`
		} `json:"packages"`
	}
	b, err := os.ReadFile("vendor/vendor.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &lock); err != nil {
		t.Fatal(err)
	}
	vendor := map[string]string{}
	for _, p := range lock.Packages {
		for spec, file := range p.Imports {
			src, err := os.ReadFile(filepath.Join("vendor", p.Dir, file))
			if err != nil {
				t.Fatal(err)
			}
			vendor[spec] = string(src)
		}
	}
	return files, vendor
}

func TestImportsNameRealExports(t *testing.T) {
	files, vendor := loadSources(t)
	for _, b := range checkImports(files, vendor) {
		t.Error(b)
	}
	n := 0
	for p, src := range files {
		if !strings.HasSuffix(p, ".d.ts") {
			n += len(importRe.FindAllString(src, -1))
		}
	}
	if n < 100 {
		t.Errorf("found only %d import statements; is the parser broken?", n)
	}
}

// The checker itself catches a missing value, a missing type, a missing
// vendored name and a missing protocol type.
func TestImportCheckCatchesMissingNames(t *testing.T) {
	files := map[string]string{
		"src/a.ts":   "export const one = 1;\nexport interface Two {}\nexport { three as four };\n",
		"src/b.ts":   `import { one, type Two, four } from "./a";` + "\n" + `import { h } from "preact";` + "\n",
		"src/c.ts":   `import { nope } from "./a";`,
		"src/d.ts":   `import type { Gone } from "./a";`,
		"src/e.ts":   `import { useNothing } from "preact/hooks";`,
		"src/f/g.ts": `import type { Meta, Missing } from "../../../../webproto/proto";`,
		"src/h.ts":   `import { effect } from "@preact/signals";`,
	}
	files[path.Join("src", "../../../webproto/proto.d.ts")] = "export interface Meta {}\n"
	vendor := map[string]string{
		"preact":               `export{k as h}`,
		"preact/hooks":         `export{d as useState}`,
		"@preact/signals":      `export{effect}from"@preact/signals-core"`,
		"@preact/signals-core": `export{j as effect}`,
	}
	got := strings.Join(checkImports(files, vendor), "\n")
	for _, want := range []string{"does not export nope", "does not export Gone", "does not export useNothing", "does not export Missing"} {
		if !strings.Contains(got, want) {
			t.Errorf("checker missed %q; it said:\n%s", want, got)
		}
	}
	for _, ok := range []string{"src/b.ts", "src/h.ts"} {
		if strings.Contains(got, ok+":") {
			t.Errorf("checker refused good imports in %s:\n%s", ok, got)
		}
	}
}

// guardFields maps each field the guard reads to the message kind it
// reads it for: str(v, "conn") inside `case "meta":` → meta.conn, and
// str(v.ui, "theme") → meta.ui.theme.
func guardFields(src string) map[string][]string {
	out := map[string][]string{}
	fn := src[strings.Index(src, "export function checkServerMsg"):]
	fn = fn[:strings.Index(fn, "\n}\n")]
	caseRe := regexp.MustCompile(`case "([a-z]+)":`)
	read := regexp.MustCompile(`\b(?:str|uint|optUint)\(v(\.[a-z_]+)?, "([a-z_]+)"|\bv\.([a-z_]+)\b|"([a-z_]+)" in v\b`)
	locs := caseRe.FindAllStringSubmatchIndex(fn, -1)
	for i, l := range locs {
		kind := fn[l[2]:l[3]]
		end := len(fn)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		for _, m := range read.FindAllStringSubmatch(fn[l[1]:end], -1) {
			switch {
			case m[2] != "" && m[1] != "":
				out[kind] = append(out[kind], m[1][1:]+"."+m[2])
			case m[2] != "":
				out[kind] = append(out[kind], m[2])
			case m[3] != "":
				out[kind] = append(out[kind], m[3])
			case m[4] != "":
				out[kind] = append(out[kind], m[4])
			}
		}
	}
	return out
}

// jsonField finds a field by its JSON name on a struct type; "a.b" walks in.
func jsonField(t reflect.Type, name string) bool {
	head, rest, nested := strings.Cut(name, ".")
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if tag != head {
			continue
		}
		if !nested {
			return true
		}
		ft := f.Type
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		return ft.Kind() == reflect.Struct && jsonField(ft, rest)
	}
	return false
}

// Every field the guard reads is a field of the Go message, by its JSON
// name, so the guard can never insist on a field the hub does not send.
func TestGuardReadsOnlyWebprotoFields(t *testing.T) {
	b, err := os.ReadFile("src/proto/guard.ts")
	if err != nil {
		t.Fatal(err)
	}
	types := map[string]reflect.Type{}
	for _, kind := range webproto.ServerKinds() {
		m, err := webproto.DecodeServer([]byte(`{"t":"` + kind + `"}`))
		if err != nil {
			t.Fatalf("webproto cannot make a %s: %v", kind, err)
		}
		types[kind] = reflect.TypeOf(m).Elem()
	}
	read := guardFields(string(b))
	if len(read["meta"]) < 5 || len(read["snap"]) < 2 {
		t.Fatalf("parsed too little of guard.ts: %v", read)
	}
	for kind, fields := range read {
		typ, ok := types[kind]
		if !ok {
			t.Errorf("guard.ts checks kind %q, which webproto does not send", kind)
			continue
		}
		for _, f := range fields {
			if f == "t" {
				continue
			}
			if !jsonField(typ, f) {
				t.Errorf("guard.ts reads %s.%s; webproto.%s has no such JSON field", kind, f, typ.Name())
			}
		}
	}
}

func TestGuardFieldsParser(t *testing.T) {
	src := "export function checkServerMsg(v) {\n  switch (v.t) {\n    case \"meta\": {\n      str(v, \"conn\");\n      str(v.ui, \"theme\");\n      if (!(\"data\" in v)) x;\n    }\n    case \"pong\":\n      str(v, \"n\");\n  }\n}\n"
	got := guardFields(src)
	if strings.Join(got["meta"], ",") != "conn,ui.theme,data" {
		t.Errorf("meta fields %v", got["meta"])
	}
	if strings.Join(got["pong"], ",") != "n" {
		t.Errorf("pong fields %v", got["pong"])
	}
	if jsonField(reflect.TypeOf(webproto.Meta{}), "ui.nope") || !jsonField(reflect.TypeOf(webproto.Meta{}), "ui.theme") {
		t.Error("jsonField walks nested fields wrong")
	}
}
