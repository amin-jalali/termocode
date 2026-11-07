package dbclient

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// JSON import (DBX-19): a JSON array of objects, or JSON lines (one
// object per line), becomes a CSV file with a header row, so it goes
// through the CSV import's preview, mapping and one-transaction insert.
// The columns are the keys in the order they are first seen; a missing
// key or null is an empty cell; a nested array or object is its JSON text.

// jsonImportMax caps the file the conversion reads (it keeps the rows in
// memory to learn every key first).
const jsonImportMax = 256 << 20

type jsonRow struct {
	keys []string
	vals map[string]string
}

func jsonImportCell(raw json.RawMessage) string {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 || string(t) == "null" {
		return ""
	}
	if t[0] == '"' {
		var s string
		if json.Unmarshal(t, &s) == nil {
			return s
		}
	}
	return string(t)
}

// readJSONObject reads one object keeping its key order.
func readJSONObject(dec *json.Decoder) (jsonRow, error) {
	tok, err := dec.Token()
	if err != nil {
		return jsonRow{}, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return jsonRow{}, errors.New("each row must be a JSON object")
	}
	r := jsonRow{vals: map[string]string{}}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return r, err
		}
		key, _ := kt.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return r, err
		}
		if _, dup := r.vals[key]; !dup {
			r.keys = append(r.keys, key)
		}
		r.vals[key] = jsonImportCell(raw)
	}
	if _, err := dec.Token(); err != nil { // the closing }
		return r, err
	}
	return r, nil
}

func readJSONRows(data []byte) ([]jsonRow, error) {
	trim := bytes.TrimSpace(data)
	if len(trim) == 0 {
		return nil, errors.New("the file is empty")
	}
	var rows []jsonRow
	if trim[0] == '[' {
		dec := json.NewDecoder(bytes.NewReader(trim))
		dec.UseNumber()
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		for dec.More() {
			r, err := readJSONObject(dec)
			if err != nil {
				return nil, fmt.Errorf("row %d: %w", len(rows)+1, err)
			}
			rows = append(rows, r)
		}
		return rows, nil
	}
	sc := bufio.NewScanner(bytes.NewReader(trim))
	sc.Buffer(make([]byte, 64<<10), jsonImportMax)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		dec := json.NewDecoder(strings.NewReader(text))
		dec.UseNumber()
		r, err := readJSONObject(dec)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		rows = append(rows, r)
	}
	return rows, sc.Err()
}

// JSONToCSV converts the JSON file src into the CSV file dst (0600) and
// returns the number of rows.
func JSONToCSV(src, dst string) (int, error) {
	f, err := os.Open(src)
	if err != nil {
		return 0, errors.New(pathErr(err))
	}
	data, err := io.ReadAll(io.LimitReader(f, jsonImportMax+1))
	f.Close()
	if err != nil {
		return 0, err
	}
	if len(data) > jsonImportMax {
		return 0, errors.New("the JSON file is larger than 256 MB")
	}
	rows, err := readJSONRows(data)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, errors.New("the file has no rows")
	}
	var cols []string
	seen := map[string]bool{}
	for _, r := range rows {
		for _, k := range r.keys {
			if !seen[k] {
				seen[k] = true
				cols = append(cols, k)
			}
		}
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	w := csv.NewWriter(out)
	_ = w.Write(cols)
	rec := make([]string, len(cols))
	for _, r := range rows {
		for i, c := range cols {
			rec[i] = r.vals[c]
		}
		_ = w.Write(rec)
	}
	w.Flush()
	if err := w.Error(); err != nil {
		out.Close()
		return 0, err
	}
	return len(rows), out.Close()
}
