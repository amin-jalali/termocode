package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/amin-jalali/termocode/internal/chatbridge"
	"github.com/amin-jalali/termocode/internal/workflow/wire"
)

// `termocode chat telegram|discord` (OMX-42): ask termocode's AI from a
// chat app. A bot of the owner's, spoken to over the service's HTTP
// API; only the user ids on --allow get answers; each message is one
// read-only AI turn (the tools of `termocode ai -x --tools read`). The
// bot token lives in the secret store (chat.<service>.token).

const chatUsage = `termocode chat: ask termocode's AI from Telegram or Discord

  termocode chat telegram --allow <user-id,…> [options]
  termocode chat discord --channel <channel-id,…> --allow <user-id,…> [options]
        run your bot until Ctrl+C. Each message from an allowed user is
        one AI turn in the folder that only reads (it never changes a
        file); the answer goes back to the chat. Write /whoami to the bot
        to see your user id.
  termocode chat token telegram|discord
        save the bot token in termocode's secret store (typed hidden, or
        one line on stdin). The token is never an argument.

Options:
  -C, --dir <folder>   the workspace the AI reads (default: here)
  -m, --model <id>     the model (default: the configured chat model)
  --tools read|none    read (default): the tools that only read; none: no tools
`

// chatTurnOpts is how one question runs.
type chatTurnOpts struct {
	dir, tools, model string
}

// Seams for tests.
var (
	chatSecretGet = func(name string) (string, bool, error) {
		st, err := openMCPSecretStore()
		if err != nil {
			return "", false, err
		}
		if st.Locked() {
			return "", false, errors.New("the secret store is locked: unlock it in termocode first")
		}
		return st.Get(name)
	}
	chatSecretSet = func(name, value string) error {
		st, err := openMCPSecretStore()
		if err != nil {
			return err
		}
		if st.Locked() {
			return errors.New("the secret store is locked: unlock it in termocode first")
		}
		return st.Set(name, value)
	}
	// chatBase overrides a service's API address (tests).
	chatBase   = map[string]string{}
	chatAITurn = defaultChatAITurn
	chatRunAI  = func(ctx context.Context, args []string, in io.Reader, out, errw io.Writer) int {
		return runAIIn(ctx, args, in, out, errw, os.Getenv)
	}
)

// chatTokenName is the secret that holds a service's bot token.
func chatTokenName(service string) string { return "chat." + service + ".token" }

func runChat(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runChatIn(ctx, args, os.Stdin, os.Stdout, os.Stderr)
}

func runChatIn(ctx context.Context, args []string, in io.Reader, out, errw io.Writer) int {
	bad := func(f string, a ...any) int {
		fmt.Fprintf(errw, "termocode chat: "+f+"\n\n%s", append(a, chatUsage)...)
		return 2
	}
	fail := func(f string, a ...any) int {
		fmt.Fprintf(errw, "termocode chat: "+f+"\n", a...)
		return 1
	}
	if len(args) == 0 {
		fmt.Fprint(errw, chatUsage)
		return 2
	}
	switch args[0] {
	case "-h", "--help", "help":
		fmt.Fprint(out, chatUsage)
		return 0
	case "token":
		if len(args) != 2 || (args[1] != "telegram" && args[1] != "discord") {
			return bad("say which bot: termocode chat token telegram|discord")
		}
		tok, err := chatReadToken(in, errw, args[1])
		if err != nil {
			return fail("%v", err)
		}
		if err := chatSecretSet(chatTokenName(args[1]), tok); err != nil {
			return fail("could not save the token: %v", err)
		}
		fmt.Fprintf(out, "Saved the %s bot token in the secret store (%s).\n", args[1], chatTokenName(args[1]))
		return 0
	case "telegram", "discord":
	default:
		return bad("unknown service %q: telegram or discord", args[0])
	}
	service := args[0]
	o := chatTurnOpts{dir: ".", tools: "read"}
	var allow, channels []string
	list := func(s string) []string {
		var r []string
		for _, p := range strings.Split(s, ",") {
			if p = strings.TrimSpace(p); p != "" {
				r = append(r, p)
			}
		}
		return r
	}
	for i := 1; i < len(args); i++ {
		a := args[i]
		name, val, hasVal := strings.Cut(a, "=")
		if !hasVal {
			if i+1 >= len(args) {
				return bad("%s needs a value", a)
			}
			i++
			val = args[i]
		}
		switch name {
		case "--allow":
			allow = append(allow, list(val)...)
		case "--channel":
			channels = append(channels, list(val)...)
		case "-C", "--dir":
			o.dir = val
		case "-m", "--model":
			o.model = val
		case "--tools":
			if val != "read" && val != "none" {
				return bad("--tools must be read or none (a chat never gets the tools that change files)")
			}
			o.tools = val
		default:
			return bad("unknown option %s", a)
		}
	}
	if len(allow) == 0 {
		return bad("add --allow <your user id> (write /whoami to the bot to see it): a bot is public, anyone can write to it")
	}
	if service == "discord" && len(channels) == 0 {
		return bad("discord needs --channel <channel id>: the channels the bot reads")
	}
	dir, err := filepath.Abs(o.dir)
	if err == nil {
		var fi os.FileInfo
		if fi, err = os.Stat(dir); err == nil && !fi.IsDir() {
			err = errors.New("not a folder")
		}
	}
	if err != nil {
		return bad("--dir %s: %v", o.dir, err)
	}
	o.dir = dir
	tok, ok, err := chatSecretGet(chatTokenName(service))
	switch {
	case err != nil:
		return fail("could not read the bot token: %v", err)
	case !ok || strings.TrimSpace(tok) == "":
		return fail("no %s bot token yet: save it with `termocode chat token %s`", service, service)
	}
	tok = strings.TrimSpace(tok)
	var conn chatbridge.Connector
	pause := time.Duration(0)
	if service == "telegram" {
		conn = &chatbridge.Telegram{Token: tok, Base: chatBase[service]}
	} else {
		conn = &chatbridge.Discord{Token: tok, Base: chatBase[service], Channels: channels}
		pause = 3 * time.Second
	}
	b := &chatbridge.Bridge{
		Conn:   conn,
		Allow:  allow,
		Pause:  pause,
		Redact: wire.RedactOutside,
		Log: func(s string) {
			fmt.Fprintf(errw, "%s %s\n", time.Now().Format("15:04:05"), strings.ReplaceAll(s, tok, "<token>"))
		},
		Handle: func(ctx context.Context, m chatbridge.Message) (string, error) {
			return chatAITurn(ctx, o, m.Text)
		},
	}
	fmt.Fprintf(errw, "termocode chat: %s bot for %s (Ctrl+C stops it)\n", service, dir)
	if err := b.Run(ctx); err != nil {
		return fail("%s", strings.ReplaceAll(err.Error(), tok, "<token>"))
	}
	return 0
}

// chatReadToken reads a bot token: hidden when stdin is a terminal,
// else one line.
func chatReadToken(in io.Reader, errw io.Writer, service string) (string, error) {
	var tok string
	if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprintf(errw, "The %s bot token (hidden): ", service)
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(errw)
		if err != nil {
			return "", err
		}
		tok = string(b)
	} else {
		line, err := bufio.NewReader(io.LimitReader(in, 4<<10)).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		tok = line
	}
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return "", errors.New("the token is empty")
	}
	return tok, nil
}

// defaultChatAITurn runs one `termocode ai -x` turn and returns its
// answer, or the reason it failed.
func defaultChatAITurn(ctx context.Context, o chatTurnOpts, question string) (string, error) {
	args := []string{"-x", question, "--output", "text", "--dir", o.dir, "--tools", o.tools}
	if o.model != "" {
		args = append(args, "--model", o.model)
	}
	var out, errw bytes.Buffer
	if code := chatRunAI(ctx, args, strings.NewReader(""), &out, &errw); code != aiExitOK {
		msg := "the AI turn ended with code " + fmt.Sprint(code)
		lines := strings.Split(strings.TrimSpace(errw.String()), "\n")
		if last := strings.TrimPrefix(lines[len(lines)-1], "termocode ai: "); strings.TrimSpace(last) != "" {
			msg = last
		}
		if strings.TrimSpace(out.String()) != "" && code == aiExitStepLimit {
			return strings.TrimSpace(out.String()) + "\n\n(" + msg + ")", nil
		}
		return "", errors.New(msg)
	}
	return strings.TrimSpace(out.String()), nil
}
