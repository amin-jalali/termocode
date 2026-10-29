package lexers

import (
	"embed"
	"io/fs"
	"sync"

	"github.com/alecthomas/chroma/v2"
)

//go:embed embedded
var embedded embed.FS

// termocode patch: the registry is filled on first use, not at package
// init. Reading the config of every embedded lexer (268 XML files) was
// most of termocode's startup init (~9 ms, 2.5 MB). Lexers registered
// before the first use wait in pending and are added after the XML
// ones, the same order as upstream's init.
var (
	globalRegistry = chroma.NewLexerRegistry()
	fillOnce       sync.Once
	pendingMu      sync.Mutex
	filled         bool
	pending        []chroma.Lexer
	afterFill      []func(*chroma.LexerRegistry)
)

// GlobalLexerRegistry is the global LexerRegistry of Lexers, filled on
// first use (termocode patch: a func, upstream has a var).
func GlobalLexerRegistry() *chroma.LexerRegistry {
	fillOnce.Do(fillRegistry)
	return globalRegistry
}

func fillRegistry() {
	paths, err := fs.Glob(embedded, "embedded/*.xml")
	if err != nil {
		panic(err)
	}
	for _, path := range paths {
		globalRegistry.Register(chroma.MustNewXMLLexer(embedded, path))
	}
	pendingMu.Lock()
	for _, lexer := range pending {
		globalRegistry.Register(lexer)
	}
	hooks := afterFill
	pending, afterFill, filled = nil, nil, true
	pendingMu.Unlock()
	for _, hook := range hooks {
		hook(globalRegistry)
	}
}

// registryFilled reports whether the registry was filled (tests).
func registryFilled() bool {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	return filled
}

// onFill runs hook once the registry is filled (package init uses it
// instead of Get, which would fill the registry at init).
func onFill(hook func(*chroma.LexerRegistry)) {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	if filled {
		hook(globalRegistry)
		return
	}
	afterFill = append(afterFill, hook)
}

// Names of all lexers, optionally including aliases.
func Names(withAliases bool) []string {
	return GlobalLexerRegistry().Names(withAliases)
}

// Aliases of all the lexers, and skip those lexers who do not have any aliases,
// or show their name instead
func Aliases(skipWithoutAliases bool) []string {
	return GlobalLexerRegistry().Aliases(skipWithoutAliases)
}

// Get a Lexer by name, alias or file extension.
//
// Note that this if there isn't an exact match on name or alias, this will
// call Match(), so it is not efficient.
func Get(name string) chroma.Lexer {
	return GlobalLexerRegistry().Get(name)
}

// MatchMimeType attempts to find a lexer for the given MIME type.
func MatchMimeType(mimeType string) chroma.Lexer {
	return GlobalLexerRegistry().MatchMimeType(mimeType)
}

// Match returns the first lexer matching filename.
//
// Note that this iterates over all file patterns in all lexers, so it's not
// particularly efficient.
func Match(filename string) chroma.Lexer {
	return GlobalLexerRegistry().Match(filename)
}

// Register a Lexer with the global registry.
func Register(lexer chroma.Lexer) chroma.Lexer {
	pendingMu.Lock()
	if !filled {
		// termocode patch: added when the registry is filled; the
		// registry is set now for lexers used directly before that.
		lexer.SetRegistry(globalRegistry)
		pending = append(pending, lexer)
		pendingMu.Unlock()
		return lexer
	}
	pendingMu.Unlock()
	return globalRegistry.Register(lexer)
}

// Analyse text content and return the "best" lexer..
func Analyse(text string) chroma.Lexer {
	return GlobalLexerRegistry().Analyse(text)
}

// PlaintextRules is used for the fallback lexer as well as the explicit
// plaintext lexer.
func PlaintextRules() chroma.Rules {
	return chroma.Rules{
		"root": []chroma.Rule{
			{`.+`, chroma.Text, nil},
			{`\n`, chroma.Text, nil},
		},
	}
}

// Fallback lexer if no other is found.
var Fallback chroma.Lexer = chroma.MustNewLexer(&chroma.Config{
	Name:      "fallback",
	Filenames: []string{"*"},
	Priority:  -1,
}, PlaintextRules)
