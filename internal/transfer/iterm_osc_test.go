package transfer

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"testing"
)

// cancelReader returns first, then cancels ctx and returns rest.
type cancelReader struct {
	first, rest string
	cancel      context.CancelFunc
	step        int
}

func (c *cancelReader) Read(p []byte) (int, error) {
	c.step++
	switch c.step {
	case 1:
		return copy(p, c.first), nil
	case 2:
		c.cancel()
		return copy(p, c.rest), nil
	}
	return 0, io.EOF
}

func TestReadITermUploadAnswers(t *testing.T) {
	good := base64.StdEncoding.EncodeToString([]byte("tgz-bytes"))
	cases := map[string]struct {
		in      string
		wantErr string // "" = success
		want    string
	}{
		"empty lines first":   {in: "\r\n\nok\n" + good + "\n\n", want: "tgz-bytes"},
		"esc before answer":   {in: "\x1b", wantErr: ErrCanceled.Error()},
		"ctrl+c in data":      {in: "ok\nAAAA\x03", wantErr: ErrCanceled.Error()},
		"strange answer":      {in: "maybe\n", wantErr: "unexpected answer"},
		"endless answer":      {in: strings.Repeat("o", 100), wantErr: "unexpected answer"},
		"input ends early":    {in: "o", wantErr: "upload: EOF"},
		"input ends in data":  {in: "ok\n" + good, wantErr: "upload: EOF"},
		"bad base64":          {in: "ok\n!!!!\n\n", wantErr: "bad data"},
		"esc inside answer":   {in: "o\x1bk\n", wantErr: "unexpected answer"},
		"crlf in data":        {in: "ok\r\n" + good + "\r\n\r\n", want: "tgz-bytes"},
		"leading data blanks": {in: "ok\n\n\n" + good + "\n\n", want: "tgz-bytes"},
	}
	for name, c := range cases {
		got, err := ReadITermUpload(context.Background(), strings.NewReader(c.in), 1<<20)
		if c.wantErr == "" {
			if err != nil || string(got) != c.want {
				t.Errorf("%s: got %q, %v", name, got, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.wantErr) {
			t.Errorf("%s: err = %v, want %q", name, err, c.wantErr)
		}
	}
}

func TestReadITermUploadCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadITermUpload(ctx, strings.NewReader("ok\n"), 0); !errors.Is(err, ErrCanceled) {
		t.Errorf("canceled before: %v", err)
	}
	// Canceled while the data comes in.
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	r := &cancelReader{first: "ok\n", rest: "AAAA\n", cancel: cancel}
	if _, err := ReadITermUpload(ctx, r, 0); !errors.Is(err, ErrCanceled) {
		t.Errorf("canceled during data: %v", err)
	}
}

func TestParseOSCPartialAndEscapes(t *testing.T) {
	// An unfinished sequence at the end is not returned yet.
	got := ParseOSC([]byte("\x1b]5113;a=1\x07\x1b]5113;b=2"), "5113")
	if len(got) != 1 || string(got[0]) != "a=1" {
		t.Fatalf("got %q", got)
	}
	// ESC that is not ESC \ is part of the payload; a lone ESC at the
	// very end may still become ESC \.
	got = ParseOSC([]byte("\x1b]5113;x\x1by\x1b\\"), "5113")
	if len(got) != 1 || string(got[0]) != "x\x1by" {
		t.Fatalf("got %q", got)
	}
	if got := ParseOSC([]byte("\x1b]5113;x\x1b"), "5113"); len(got) != 0 {
		t.Fatalf("lone ESC: got %q", got)
	}
}

func TestDecodeITermFileBadInput(t *testing.T) {
	for in, want := range map[string]string{
		"Other=1:AAAA":                    "not a File",
		"File=name=YQ==;size=1":           "no data",
		"File=name=!!!;size=1:AAAA":       "illegal base64",
		"File=name=YQ;size=1:" + B64("x"): "", // name without padding
	} {
		name, data, err := DecodeITermFile([]byte(in))
		if want == "" {
			if err != nil || name != "a" || string(data) != "x" {
				t.Errorf("%q: %q %q %v", in, name, data, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err = %v, want %q", in, err, want)
		}
	}
}

func TestScpTargets(t *testing.T) {
	cases := []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{"LOGNAME": "amin"}, "scp 'amin@devbox:/a' ."},
		{map[string]string{}, "scp 'devbox:/a' ."},
		{map[string]string{"USER": "u", "SSH_CONNECTION": "fe80::1 5000 2001:db8::7 22"}, "scp 'u@[2001:db8::7]:/a' ."},
	}
	for _, c := range cases {
		if got := ScpDownload(envOf(c.env), "devbox", "/a", false); got != c.want {
			t.Errorf("%v: %q, want %q", c.env, got, c.want)
		}
	}
}
