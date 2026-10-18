package jsonc

import (
	"encoding/json"
	"testing"
)

func TestStrip(t *testing.T) {
	got := Strip([]byte(`{// comment
"url":"https://x/*y*/", "n": [1,],}`))
	var v map[string]any
	if err := json.Unmarshal(got, &v); err != nil {
		t.Fatalf("Strip produced invalid JSON: %s: %v", got, err)
	}
	if v["url"] != "https://x/*y*/" {
		t.Fatalf("string changed: %#v", v["url"])
	}
}
