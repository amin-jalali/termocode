package git

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// BlameRangeLine is who last changed one line of a file.
type BlameRangeLine struct {
	Line    int    // 1-based line number in the working file
	Hash    string // short sha, or "uncommitted"
	Author  string
	Date    string // author date, YYYY-MM-DD ("" when uncommitted)
	Subject string
}

// BlameRange runs one `git blame --porcelain` for lines start..end
// (1-based, inclusive) of path. end <= 0 means "to the end of the file".
// A start past the end of the file is an error (git refuses it).
func BlameRange(dir, path string, start, end int) ([]BlameRangeLine, error) {
	if start < 1 {
		start = 1
	}
	rng := fmt.Sprintf("%d,", start)
	if end > 0 {
		if end < start {
			return nil, fmt.Errorf("end %d is before start %d", end, start)
		}
		// "+count" lets git clamp an end past the last line.
		rng += "+" + strconv.Itoa(end-start+1)
	}
	out, err := runCombined(dir, "git", "blame", "--porcelain", "-L", rng, "--", path)
	if err != nil {
		return nil, err
	}
	return parseBlameRange(out), nil
}

// parseBlameRange reads porcelain output: a "<sha> <orig> <final> [n]"
// header per line, commit headers only the first time a sha shows up,
// then the line itself after a tab.
func parseBlameRange(s string) []BlameRangeLine {
	type info struct{ author, date, subject string }
	commits := map[string]*info{}
	var out []BlameRangeLine
	var cur *BlameRangeLine
	var sha string
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, "\t") {
			if cur != nil {
				c := commits[sha]
				if strings.Trim(sha, "0") == "" {
					cur.Hash = "uncommitted"
				} else if c != nil {
					cur.Author, cur.Date, cur.Subject = c.author, c.date, c.subject
				}
				out = append(out, *cur)
				cur = nil
			}
			continue
		}
		if cur == nil {
			f := strings.Fields(line)
			if len(f) >= 3 && len(f[0]) >= 7 {
				n, err := strconv.Atoi(f[2])
				if err != nil {
					continue
				}
				sha = f[0]
				cur = &BlameRangeLine{Line: n, Hash: sha[:7]}
				if commits[sha] == nil {
					commits[sha] = &info{}
				}
			}
			continue
		}
		c := commits[sha]
		switch {
		case strings.HasPrefix(line, "author "):
			c.author = strings.TrimPrefix(line, "author ")
		case strings.HasPrefix(line, "author-time "):
			if sec, err := strconv.ParseInt(strings.TrimPrefix(line, "author-time "), 10, 64); err == nil {
				c.date = time.Unix(sec, 0).Format("2006-01-02")
			}
		case strings.HasPrefix(line, "summary "):
			c.subject = strings.TrimPrefix(line, "summary ")
		}
	}
	return out
}

// runCombined runs a command and returns stdout; on failure the error
// carries git's message.
func runCombined(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", cmdError(err, []byte(stderr.String()))
	}
	return string(out), nil
}
