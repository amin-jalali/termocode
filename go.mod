module github.com/amin-jalali/termocode

go 1.26.2

require (
	filippo.io/age v1.3.1
	github.com/alecthomas/chroma/v2 v2.23.1
	github.com/charmbracelet/bubbletea v1.3.10
	github.com/charmbracelet/harmonica v0.2.0
	github.com/charmbracelet/lipgloss v1.1.1-0.20250404203927-76690c660834
	github.com/charmbracelet/x/ansi v0.11.6
	github.com/charmbracelet/x/term v0.2.2
	github.com/coder/websocket v1.8.15
	github.com/creack/pty v1.1.24
	github.com/evanw/esbuild v0.28.2
	github.com/go-sql-driver/mysql v1.8.1
	github.com/go-webauthn/webauthn v0.18.2
	github.com/gomodule/redigo v1.9.2
	github.com/jackc/pgx/v5 v5.7.2
	github.com/mattn/go-runewidth v0.0.19
	github.com/muesli/cancelreader v0.2.2
	github.com/muesli/termenv v0.16.0
	github.com/neovim/go-client v1.2.1
	github.com/sahilm/fuzzy v0.1.1
	github.com/yuin/goldmark v1.7.13
	github.com/zalando/go-keyring v0.2.6
	golang.design/x/clipboard v0.7.1
	golang.org/x/crypto v0.57.0
	golang.org/x/image v0.28.0
	golang.org/x/sys v0.48.0
	golang.org/x/term v0.46.0
	golang.org/x/text v0.42.0
	rsc.io/qr v0.2.0
)

require (
	al.essio.dev/pkg/shellescape v1.5.1 // indirect
	filippo.io/edwards25519 v1.1.0 // indirect
	filippo.io/hpke v0.4.0 // indirect
	github.com/aymanbagabas/go-osc52/v2 v2.0.1 // indirect
	github.com/charmbracelet/colorprofile v0.4.1 // indirect
	github.com/charmbracelet/x/cellbuf v0.0.15 // indirect
	github.com/clipperhouse/displaywidth v0.9.0 // indirect
	github.com/clipperhouse/stringish v0.1.1 // indirect
	github.com/clipperhouse/uax29/v2 v2.5.0 // indirect
	github.com/danieljoos/wincred v1.2.2 // indirect
	github.com/dlclark/regexp2 v1.11.5 // indirect
	github.com/erikgeiser/coninput v0.0.0-20211004153227-1c3628e74d0f // indirect
	github.com/fxamacker/cbor/v2 v2.9.4 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/go-webauthn/x v0.3.1 // indirect
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/google/go-tpm v0.9.8 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/lucasb-eyer/go-colorful v1.3.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-localereader v0.0.1 // indirect
	github.com/muesli/ansi v0.0.0-20230316100256-276c6243b2f6 // indirect
	github.com/philhofer/fwd v1.2.0 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/tinylib/msgp v1.6.4 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	golang.org/x/exp/shiny v0.0.0-20250606033433-dcc06ee1d476 // indirect
	golang.org/x/mobile v0.0.0-20250606033058-a2a15c67f36f // indirect
	golang.org/x/sync v0.23.0 // indirect
)

replace github.com/charmbracelet/bubbletea => ./third_party/bubbletea

replace github.com/alecthomas/chroma/v2 => ./third_party/chroma
