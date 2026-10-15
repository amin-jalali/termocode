package tea

import "testing"

func TestParseInput(t *testing.T) {
	msgs := ParseInput([]byte("a\x03\x1b[A\x1b[111;6u"))
	want := []string{"a", "ctrl+c", "up", "ctrl+shift+o"}
	if len(msgs) != len(want) {
		t.Fatalf("got %d msgs: %v", len(msgs), msgs)
	}
	for i, m := range msgs {
		if k, ok := m.(KeyMsg); !ok || k.String() != want[i] {
			t.Errorf("msg %d = %v, want %s", i, m, want[i])
		}
	}
}
