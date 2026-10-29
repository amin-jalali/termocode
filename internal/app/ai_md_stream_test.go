package app

import (
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// mdRandomAnswer is a random answer made of the blocks a model writes.
func mdRandomAnswer(rng *rand.Rand, n int) string {
	blocks := []string{
		"Some text with `code` and **bold** words that wraps over the line width of the panel easily.\n",
		"- item one\n- item two with `x`\n  - nested\n",
		"1. first\n2. second\n",
		"```go\nfunc f() int {\n\treturn 1\n}\n```\n",
		"```\nplain code\n\n\nwith blank lines\n```\n",
		"## A heading\n",
		"# Title\n",
		"> a quote that is long enough to wrap around the panel width once or twice\n",
		"| a | b |\n|---|---|\n| 1 | 2 |\n",
		"---\n",
		"中文 👩‍💻 🇮🇷 سلام دنیا\n",
		"\n",
		"text right after\n",
	}
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(blocks[rng.IntN(len(blocks))])
		if rng.IntN(3) > 0 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// Drawing a growing answer in parts gives exactly the rows of drawing it
// whole, for every prefix a stream can show, also when it ends.
func TestRenderAnswerPartsMatchesWhole(t *testing.T) {
	for seed := uint64(1); seed <= 40; seed++ {
		rng := rand.New(rand.NewPCG(seed, 7))
		text := mdRandomAnswer(rng, 3+rng.IntN(25))
		width := 30 + rng.IntN(80)
		s := &aiState{}
		for end := 0; end <= len(text); {
			end = min(len(text), end+1+rng.IntN(40))
			for end < len(text) && !utf8.RuneStart(text[end]) {
				end++ // a stream never cuts a rune
			}
			chunk := text[:end]
			streaming := end < len(text)
			got := s.renderAnswerParts(2, chunk, width, streaming)
			want := renderChatMarkdown(chunk, width, aiIndent, 2, streaming)
			if !reflect.DeepEqual(got, want) {
				for k := 0; k < max(len(got), len(want)); k++ {
					if k >= len(got) || k >= len(want) || !reflect.DeepEqual(got[k], want[k]) {
						t.Logf("row %d differs (got %d rows, want %d)", k, len(got), len(want))
						if k < len(got) && k < len(want) {
							t.Logf("got  %+v\nwant %+v", got[k], want[k])
						}
						break
					}
				}
				t.Fatalf("seed %d width %d end %d: parts differ from whole\ntext %q", seed, width, end, chunk)
			}
			if end == len(text) {
				break
			}
		}
	}
}

// Each new chunk of a long streaming answer costs about the same, not
// more and more: a 1 MB answer in 4 KB chunks is drawn in well under a
// second in all (it took about a minute when every chunk redrew the
// whole answer).
func TestRenderAnswerPartsStaysCheapWhileStreaming(t *testing.T) {
	rng := rand.New(rand.NewPCG(99, 7))
	var b strings.Builder
	for b.Len() < 1<<20 {
		b.WriteString(mdRandomAnswer(rng, 20))
	}
	text := b.String()
	s := &aiState{}
	t0 := time.Now()
	var worst time.Duration
	for end := 4096; ; end += 4096 {
		end = min(end, len(text))
		for end < len(text) && !utf8.RuneStart(text[end]) {
			end++
		}
		t1 := time.Now()
		s.renderAnswerParts(1, text[:end], 120, end < len(text))
		worst = max(worst, time.Since(t1))
		if end == len(text) {
			break
		}
	}
	t.Logf("1 MB in 4 KB chunks: %v, worst chunk %v", time.Since(t0), worst)
	if bound := 200 * time.Millisecond; worst > bound && !raceEnabled {
		t.Errorf("worst chunk took %v, bound %v", worst, bound)
	}
}
