package httpclient

import (
	"path/filepath"
	"strings"
)

// HTTP is the imported collection as one `.http` file: a note, the
// `@name = value` variables, then one `###` block per request (API-15).
func (im Imported) HTTP() string {
	var b strings.Builder
	what := map[string]string{FormatCollection: "a collection JSON", FormatHAR: "a HAR file", FormatInsomnia: "an Insomnia export"}[im.Format]
	if what == "" {
		what = "a collection"
	}
	b.WriteString("# Imported from " + what + ".\n")
	if len(im.Placeholders) > 0 {
		names := make([]string, len(im.Placeholders))
		for i, p := range im.Placeholders {
			names[i] = "{{" + p + "}}"
		}
		b.WriteString("# Secret values were not copied: set " + strings.Join(names, ", ") + " in http-client.private.env.json.\n")
	}
	if len(im.Vars) > 0 {
		b.WriteString("\n")
		for _, v := range im.Vars {
			b.WriteString("@" + v.Name + " = " + oneLine(v.Value) + "\n")
		}
	}
	b.WriteString("\n")
	if len(im.Requests) > 0 && im.Requests[0].Name == "" {
		b.WriteString("###\n")
	}
	b.WriteString(Serialize(im.Requests))
	return b.String()
}

// ImportedFileName is the `.http` name for an imported file: its name
// without the extensions, letters, digits, '.', '_' and '-' only.
func ImportedFileName(src string) string {
	base := filepath.Base(src)
	for _, ext := range []string{".json", ".har", ".postman_collection", ".insomnia", ".yaml", ".yml"} {
		base = strings.TrimSuffix(base, ext)
	}
	var b strings.Builder
	dash := false
	for _, r := range base {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '.'
		if !ok {
			dash = true
			continue
		}
		if dash && b.Len() > 0 {
			b.WriteByte('-')
		}
		dash = false
		b.WriteRune(r)
	}
	s := strings.Trim(b.String(), ".-")
	if len(s) > 60 {
		s = strings.TrimRight(s[:60], ".-")
	}
	if s == "" {
		s = "imported"
	}
	return s + ".http"
}

// HARFileName is the default export path for an `.http` file.
func HARFileName(httpPath string) string {
	return strings.TrimSuffix(strings.TrimSuffix(httpPath, ".http"), ".rest") + ".har"
}
