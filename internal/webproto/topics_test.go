package webproto

import "testing"

func TestParseTopic(t *testing.T) {
	good := map[string]Topic{
		"status":     {Name: "status"},
		"chat":       {Name: "chat"},
		"buf:12":     {Name: "buf", Key: "12"},
		"term:7":     {Name: "term", Key: "7"},
		"containers": {Name: "containers"},
	}
	for in, want := range good {
		got, err := ParseTopic(in)
		if err != nil {
			t.Errorf("ParseTopic(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseTopic(%q) = %+v, want %+v", in, got, want)
		}
		if got.String() != in {
			t.Errorf("String() = %q, want %q", got.String(), in)
		}
	}
	bad := []string{
		"", "nope", "buf", "buf:", "buf:x", "buf:-1", "buf:01", "buf:0",
		"buf:99999999999", "status:1", "term:1:2", "Buf:1", " status",
		"__proto__", "buf:__proto__",
	}
	for _, in := range bad {
		if _, err := ParseTopic(in); err == nil {
			t.Errorf("ParseTopic(%q) accepted", in)
		}
	}
}

func TestTopicTableKeyedAndPhased(t *testing.T) {
	keyed := map[string]bool{"buf": true, "term": true}
	for _, d := range Topics() {
		if d.Keyed != keyed[d.Name] {
			t.Errorf("topic %s keyed=%v", d.Name, d.Keyed)
		}
		if d.Phase != PhaseV1 && d.Phase != PhaseV2 {
			t.Errorf("topic %s has phase %q", d.Name, d.Phase)
		}
	}
}
