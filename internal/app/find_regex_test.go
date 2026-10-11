package app

import "testing"

// Spec: Given a JavaScript-style regex typed in the find bar (.* on),
// when it is turned into an nvim pattern,
// then operators stay operators and JS-literal characters stay literal.
func TestJSRegexToVim(t *testing.T) {
	cases := map[string]string{
		`fo+b`:         `\vfo+b`,
		`(a|b)?c`:      `\v(a|b)?c`,
		`\d{2,3}`:      `\v\d{2,3}`,
		`<div id="x">`: `\v\<div id\="x"\>`,
		`a/b`:          `\va\/b`,
		`\bword\b`:     `\v%(<|>)word%(<|>)`,
		`(?:x)`:        `\v%(x)`,
		`a.*?b`:        `\va.{-}b`,
		`[\d_]`:        `\v[0-9_]`,
		`[]a]`:         `\v[\]a]`,
		`\.\(`:         `\v\.\(`,
		`50% & ~`:      `\v50\% \& \~`,
		`x@y`:          `\vx\@y`,
	}
	for in, want := range cases {
		if got := jsRegexToVim(in); got != want {
			t.Errorf("jsRegexToVim(%q) = %q, want %q", in, got, want)
		}
	}
}

// Spec: Given a VS Code regex replacement,
// when it is turned into the right side of :s,
// then $n / $& are groups, $$ is a dollar, \n a line break, and the :s
// specials (/ & ~ \) are literal.
func TestJSReplaceToVim(t *testing.T) {
	cases := map[string]string{
		`$2 at $1`: `\2 at \1`,
		`[$&]`:     `[\0]`,
		`$$5`:      `$5`,
		`a\nb`:     `a\rb`,
		`a/b&c~`:   `a\/b\&c\~`,
		`$x`:       `$x`,
	}
	for in, want := range cases {
		if got := jsReplaceToVim(in); got != want {
			t.Errorf("jsReplaceToVim(%q) = %q, want %q", in, got, want)
		}
	}
}
