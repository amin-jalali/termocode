package dbclient

import (
	"strconv"
	"strings"
)

// RedisRename is RENAMENX src dst (never overwrites), or RENAME when the
// user chose to replace an existing dst.
func RedisRename(src, dst string, overwrite bool) []string {
	if overwrite {
		return []string{"RENAME", src, dst}
	}
	return []string{"RENAMENX", src, dst}
}

// RedisCopy is COPY src dst, into database destDB when destDB >= 0, with
// REPLACE only when the user chose to overwrite dst.
func RedisCopy(src, dst string, destDB int, replace bool) []string {
	cmd := []string{"COPY", src, dst}
	if destDB >= 0 {
		cmd = append(cmd, "DB", strconv.Itoa(destDB))
	}
	if replace {
		cmd = append(cmd, "REPLACE")
	}
	return cmd
}

// RedisInfoSection is one "# Name" block of INFO, fields in server order.
type RedisInfoSection struct {
	Name   string
	Fields [][2]string
}

// ParseRedisInfo splits INFO text into sections. Lines without a colon
// are dropped; a value keeps any colons after the first.
func ParseRedisInfo(text string) []RedisInfoSection {
	var out []RedisInfoSection
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			out = append(out, RedisInfoSection{Name: strings.TrimSpace(strings.TrimPrefix(line, "#"))})
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if len(out) == 0 {
			out = append(out, RedisInfoSection{})
		}
		last := &out[len(out)-1]
		last.Fields = append(last.Fields, [2]string{k, v})
	}
	return out
}

// RedisSlowEntry is one SLOWLOG GET item. Client and Name are empty on
// servers older than 4.0.
type RedisSlowEntry struct {
	ID, Unix, Micros int64
	Args             []string
	Client, Name     string
}

// ParseRedisSlowlog reads a SLOWLOG GET reply; broken items are skipped.
func ParseRedisSlowlog(reply any) []RedisSlowEntry {
	items, _ := reply.([]any)
	var out []RedisSlowEntry
	for _, it := range items {
		f, ok := it.([]any)
		if !ok || len(f) < 4 {
			continue
		}
		id, ok1 := f[0].(int64)
		at, ok2 := f[1].(int64)
		us, ok3 := f[2].(int64)
		raw, ok4 := f[3].([]any)
		if !ok1 || !ok2 || !ok3 || !ok4 {
			continue
		}
		e := RedisSlowEntry{ID: id, Unix: at, Micros: us, Args: make([]string, 0, len(raw))}
		for _, a := range raw {
			e.Args = append(e.Args, redisText(a))
		}
		if len(f) >= 6 {
			e.Client, e.Name = redisText(f[4]), redisText(f[5])
		}
		out = append(out, e)
	}
	return out
}

func redisText(v any) string {
	switch x := v.(type) {
	case []byte:
		return string(x)
	case string:
		return x
	case int64:
		return strconv.FormatInt(x, 10)
	}
	return ""
}
