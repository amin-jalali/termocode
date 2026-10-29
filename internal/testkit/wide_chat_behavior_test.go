package testkit

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Spec (SCN-11): Given a fake AI,
// when the user asks a question with wide glyphs and the answer holds
// CJK, emoji (ZWJ, flags), accents and Persian, also in a long line
// that wraps and in a code block,
// then the chat keeps every row at the window width and cuts no glyph.
func TestWideAIChatMessages(t *testing.T) {
	answer := "## " + wideCJK + " " + wideDev + "\n\n" + strings.Repeat(wideMixed+" ", 12) +
		"\n\n```\n" + strings.Repeat("中文"+wideFlag, 40) + "\n```\n\n- " + wideFa + " " + wideDev + "\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, part := range strings.SplitAfter(answer, "\n") {
			q := strings.ReplaceAll(strings.ReplaceAll(part, "\n", `\n`), `"`, `\"`)
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"%s\"}}]}\n\n", q)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	for _, sz := range [][2]int{{80, 24}, {160, 40}} {
		t.Run(fmt.Sprintf("%dx%d", sz[0], sz[1]), func(t *testing.T) {
			d := New(t, Options{W: sz[0], H: sz[1], Files: wideFiles(),
				Setup: func(t testing.TB, _ string) {
					t.Setenv("TERMOCODE_AI_PROVIDER", "ollama")
					t.Setenv("TERMOCODE_AI_MODEL", "fake")
					t.Setenv("TERMOCODE_AI_BASE_URL", srv.URL+"/v1")
				}})
			d.Key("alt+a")
			d.Type(wideMixed + " " + wideCJK)
			checkGlyphs(d, "chat input")
			d.Key("enter")
			if !d.WaitFor(func() bool {
				s := screen(d)
				return strings.Contains(s, "Copy") && !strings.Contains(s, "generating")
			}, 5*time.Second) {
				t.Fatalf("no answer:\n%s", screen(d))
			}
			checkGlyphs(d, "chat answer")
			for i := 0; i < 6; i++ {
				d.Wheel(d.W-10, d.H/2, true)
				checkGlyphs(d, fmt.Sprintf("chat scrolled %d", i))
			}
		})
	}
}
