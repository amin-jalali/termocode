package nvim

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

func TestRegisterRequestAnswersLua(t *testing.T) {
	c := newAttachedClient(t)
	if err := c.RegisterRequest("tc_req", func(args []any) (any, error) {
		if len(args) == 2 && toString(args[0]) == "fail" {
			return nil, errors.New("no such thing")
		}
		return map[string]any{"echo": toString(args[0]), "n": len(args)}, nil
	}); err != nil {
		t.Fatalf("RegisterRequest: %v", err)
	}
	if err := c.ExecLua(`vim.g.termocode_channel = ` + strconv.Itoa(c.ChannelID())); err != nil {
		t.Fatal(err)
	}
	got, err := c.EvalLuaString(`local r = vim.rpcrequest(vim.g.termocode_channel, 'tc_req', 'hi', 2)
return r.echo .. ':' .. r.n`)
	if err != nil || got != "hi:2" {
		t.Fatalf("reply = %q, %v", got, err)
	}
	got, err = c.EvalLuaString(`local ok, e = pcall(vim.rpcrequest, vim.g.termocode_channel, 'tc_req', 'fail', 1)
return tostring(ok) .. ' ' .. vim.inspect(e)`)
	if err != nil || !strings.HasPrefix(got, "false") || !strings.Contains(got, "no such thing") {
		t.Fatalf("error reply = %q, %v", got, err)
	}
}

func TestRegisterRequestRejectsBadMethod(t *testing.T) {
	c := &Client{}
	if err := c.RegisterRequest("", func([]any) (any, error) { return nil, nil }); err == nil {
		t.Fatal("empty method accepted")
	}
	if err := c.RegisterRequest("redraw", func([]any) (any, error) { return nil, nil }); err == nil {
		t.Fatal("redraw accepted")
	}
}
