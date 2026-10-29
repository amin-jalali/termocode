package update

import (
	"strings"
	"testing"
)

const sampleRelease = `{
  "tag_name": "v0.2.0",
  "name": "termocode v0.2.0",
  "html_url": "https://github.com/amin-jalali/termocode/releases/tag/v0.2.0",
  "draft": false,
  "prerelease": false,
  "body": "notes",
  "assets": [
    {"name": "termocode-v0.2.0-linux-amd64.tar.gz", "size": 12345,
     "browser_download_url": "https://github.com/amin-jalali/termocode/releases/download/v0.2.0/termocode-v0.2.0-linux-amd64.tar.gz"},
    {"name": "checksums.txt", "size": 420,
     "browser_download_url": "https://github.com/amin-jalali/termocode/releases/download/v0.2.0/checksums.txt"}
  ]
}`

func TestParseRelease(t *testing.T) {
	r, err := ParseRelease(strings.NewReader(sampleRelease))
	if err != nil {
		t.Fatal(err)
	}
	if r.Tag != "v0.2.0" || r.HTMLURL == "" || len(r.Assets) != 2 {
		t.Fatalf("got %+v", r)
	}
	if a := r.Assets[0]; a.Name != "termocode-v0.2.0-linux-amd64.tar.gz" || a.Size != 12345 || !strings.HasPrefix(a.URL, "https://") {
		t.Fatalf("asset %+v", a)
	}
}

func TestParseReleaseRejects(t *testing.T) {
	for name, body := range map[string]string{
		"not json":   "<html>",
		"no tag":     `{"name":"x"}`,
		"bad tag":    `{"tag_name":"latest"}`,
		"draft":      `{"tag_name":"v1.0.0","draft":true}`,
		"prerelease": `{"tag_name":"v1.0.0-rc1","prerelease":true}`,
		"huge":       `{"tag_name":"v1.0.0","body":"` + strings.Repeat("x", maxReleaseJSON) + `"}`,
	} {
		if _, err := ParseRelease(strings.NewReader(body)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}
