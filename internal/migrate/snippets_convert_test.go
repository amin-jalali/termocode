package migrate

import (
	"strings"
	"testing"
)

func TestConvertSnippetBody(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"plain text", "hello world", "hello world"},
		{"tabstops", "a $1 b ${2} c $0", "a $1 b $2 c $0"},
		{"placeholder", "func ${1:name}(${2:args}) {\n\t$0\n}", "func ${1:name}(${2:args}) {\n\t$0\n}"},
		{"nested placeholder", "${1:${2:b}}", "${1:${2:b}}"},
		{"nested variable", "${1:$TM_FILENAME}", "${1:${TM_FILENAME}}"},
		{"unknown variable default next to text", "${1:a ${CURRENT_YEAR:2025}}", "${1:a 2025}"},
		{"choice", "${1|one,two,three|}", "${1|one,two,three|}"},
		{"choice escapes", `${1|a\,b,c\|d|}`, `${1|a\,b,c\|d|}`},
		{"tabstop before digit text", "${1}0 and ${2}x", "${1}0 and ${2}x"},
		{"variable", "$TM_FILENAME and ${TM_FILENAME_BASE}", "${TM_FILENAME} and ${TM_FILENAME_BASE}"},
		{"variable before word char", "$TM_FILENAME_BASE.go", "${TM_FILENAME_BASE}.go"},
		{"variable default", "${TM_DIRECTORY:src}", "${TM_DIRECTORY:src}"},
		{"all known variables", "$TM_SELECTED_TEXT$TM_CURRENT_LINE$TM_CURRENT_WORD$TM_LINE_INDEX$TM_LINE_NUMBER$TM_FILEPATH", "${TM_SELECTED_TEXT}${TM_CURRENT_LINE}${TM_CURRENT_WORD}${TM_LINE_INDEX}${TM_LINE_NUMBER}${TM_FILEPATH}"},
		{"unknown variable with default keeps the default", "year ${CURRENT_YEAR:2025}!", "year 2025!"},
		{"unknown variable default with tabstop", "${WORKSPACE_NAME:${1:app}}", "${1:app}"},
		{"escaped dollar", `cost \$5 and \$1`, `cost \$5 and \$1`},
		{"escaped brace and backslash", `\} and \\ done`, `} and \\ done`},
		{"other backslash stays literal", `a\nb`, `a\\nb`},
		{"lone dollar", "price $ 5 and $", `price \$ 5 and \$`},
		{"closing brace in text", "if x { y }", "if x { y }"},
		{"brace in default text of a variable", "${TM_FILENAME:a\\}b}", "${TM_FILENAME:a\\}b}"},
		{"escaped brace inside placeholder", `${1:a\}b}`, `${1:a\}b}`},
		{"unclosed placeholder is text", "x ${1:abc", `x \${1:abc`},
		{"empty choice is text", "${1||}", `\${1||}`},
		{"placeholder zero keeps its text", "${0:end}", "${0}end"},
		{"empty placeholder", "${1:}", "${1:}"},
		{"mirrors", "$1 = $1", "$1 = $1"},
	} {
		got, reason := ConvertSnippetBody(tc.in)
		if reason != "" || got != tc.want {
			t.Errorf("%s: %q -> %q (reason %q), want %q", tc.name, tc.in, got, reason, tc.want)
		}
	}
}

func TestConvertSnippetBodyUnsupportedConstructs(t *testing.T) {
	for _, tc := range []struct{ in, mention string }{
		{"$CURRENT_YEAR", "CURRENT_YEAR"},
		{"${RANDOM}", "RANDOM"},
		{"$UUID", "UUID"},
		{"${foo}", "foo"},
		{"${1/(.*)/${1:/upcase}/}", "transform"},
		{"${TM_FILENAME/(.*)\\..+$/$1/}", "transform"},
		{"a ${1:b ${CLIPBOARD}}", "CLIPBOARD"},
		{"${1:a ${2:b}}", "mixes text"},
		{"${1:a $2}", "mixes text"},
		{"${TM_FILENAME:a $1}", "mixes text"},
		{"echo \"$HOME\"", "HOME"},
	} {
		got, reason := ConvertSnippetBody(tc.in)
		if got != "" || !strings.Contains(reason, tc.mention) {
			t.Errorf("%q -> %q, %q; want a reason mentioning %q", tc.in, got, reason, tc.mention)
		}
	}
}

func TestReadVSCodeSnippetsConvertsBodiesAndSkipsUnsupported(t *testing.T) {
	dir := snipUser(t, map[string]string{"go.json": `{
  "Fine": {"prefix": "fine", "body": ["f ${1:x}", "$0", "}"]},
  "Date": {"prefix": "date", "body": "$CURRENT_YEAR"},
  "Fallback": {"prefix": ["fb1", "fb2"], "body": "${CURRENT_YEAR:2025}"}
}`})
	d, _ := ReadVSCode(dir)
	s, ok := snippetAt(d, "go", "fine")
	if !ok || s.Body != "f ${1:x}\n$0\n}" {
		t.Fatalf("fine = %+v", s)
	}
	if _, ok := snippetAt(d, "go", "date"); ok {
		t.Error("snippet with an unsupported variable was imported")
	}
	if !hasSkipped(d, RowSnippet, "CURRENT_YEAR") {
		t.Errorf("skipped = %+v", d.Skipped)
	}
	if s, ok := snippetAt(d, "go", "fb2"); !ok || s.Body != "2025" {
		t.Errorf("fallback = %+v", s)
	}
}
