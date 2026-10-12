package session

import (
	"reflect"
	"testing"
)

func TestAttachedFilter(t *testing.T) {
	var f attachedFilter
	out, marks := f.Filter([]byte("old\x1b[H" + AttachedSeq(3) + "new" + AttachedSeq(12)))
	if string(out) != "old\x1b[Hnew" || !reflect.DeepEqual(marks, []attachedMark{{6, 3}, {9, 12}}) {
		t.Fatalf("got %q %v", out, marks)
	}

	// Split across reads, byte by byte.
	var got []byte
	var all []attachedMark
	for _, c := range []byte("ab" + AttachedSeq(7) + "cd") {
		o, m := f.Filter([]byte{c})
		for _, x := range m {
			all = append(all, attachedMark{len(got) + x.pos, x.gen})
		}
		got = append(got, o...)
	}
	if string(got) != "abcd" || !reflect.DeepEqual(all, []attachedMark{{2, 7}}) {
		t.Fatalf("split: got %q %v", got, all)
	}

	// Not ours: other APC strings, no number, a number too long.
	for _, in := range []string{
		"\x1b_Ga=T;AAAA\x1b\\",
		"\x1b_termocode:detach\x1b\\",
		"\x1b_termocode:attached;\x1b\\",
		"\x1b_termocode:attached;x\x1b\\",
		"\x1b_termocode:attached;12345678901234567890\x1b\\",
	} {
		out, marks := f.Filter([]byte(in))
		out2, _ := f.Filter(nil)
		if string(out)+string(out2) != in || len(marks) != 0 {
			t.Errorf("%q: got %q %v", in, out, marks)
		}
	}

	// A held tail that turns out not to be ours is sent later.
	out, _ = f.Filter([]byte("z\x1b_termocode:att"))
	if string(out) != "z" {
		t.Fatalf("tail not held: %q", out)
	}
	out, marks = f.Filter([]byte("ic"))
	if string(out) != "\x1b_termocode:attic" || len(marks) != 0 {
		t.Fatalf("held tail lost: %q %v", out, marks)
	}
}
