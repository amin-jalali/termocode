package dbclient

import (
	"html"
	"io"
	"strings"
)

// htmlWriter writes one standalone HTML page with a table. Every cell is
// escaped, so a value can never become markup or script.
type htmlWriter struct {
	w     io.Writer
	title string
	cols  int
}

const htmlExportStyle = "table{border-collapse:collapse}th,td{border:1px solid #888;padding:2px 6px;white-space:pre-wrap;text-align:left}td.null{color:#888;font-style:italic}"

func (h *htmlWriter) Columns(cols []Column) error {
	h.cols = len(cols)
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html>\n<head>\n<meta charset=\"utf-8\">\n<title>" + html.EscapeString(h.title) + "</title>\n")
	b.WriteString("<style>" + htmlExportStyle + "</style>\n</head>\n<body>\n<table>\n<thead>\n<tr>")
	for _, c := range cols {
		b.WriteString("<th>" + html.EscapeString(c.Name) + "</th>")
	}
	b.WriteString("</tr>\n</thead>\n<tbody>\n")
	_, err := io.WriteString(h.w, b.String())
	return err
}

func (h *htmlWriter) Row(row []Cell) error {
	var b strings.Builder
	b.WriteString("<tr>")
	for i := 0; i < h.cols; i++ {
		switch {
		case i >= len(row):
			b.WriteString("<td></td>")
		case row[i].Null:
			b.WriteString(`<td class="null">NULL</td>`)
		default:
			b.WriteString("<td>" + html.EscapeString(row[i].Text) + "</td>")
		}
	}
	b.WriteString("</tr>\n")
	_, err := io.WriteString(h.w, b.String())
	return err
}

func (h *htmlWriter) End() error {
	_, err := io.WriteString(h.w, "</tbody>\n</table>\n</body>\n</html>\n")
	return err
}
