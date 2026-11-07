package mcp

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/blocks"
)

func formReq(fields ...ElicitField) ElicitRequest {
	return ElicitRequest{Server: "gh", Mode: ElicitForm, Message: "Which repo?", Fields: fields}
}

// MOB-12: a plain question becomes a form block with typed fields and
// the server's defaults.
func TestFormOf(t *testing.T) {
	req := formReq(
		ElicitField{Name: "repo", Title: "Repository", Required: true, Description: "owner/name"},
		ElicitField{Name: "count", Type: "integer", Default: float64(5)},
		ElicitField{Name: "draft", Type: "boolean", Default: true},
		ElicitField{Name: "color", Enum: []string{"red", "blue"}},
	)
	b, ok := FormOf("card-1", req, nil)
	if !ok {
		t.Fatal("not a form")
	}
	if err := b.Validate(); err != nil || b.Kind != blocks.KindForm || b.ForYou != "Which repo?" {
		t.Fatalf("block %+v: %v", b, err)
	}
	kinds := []string{blocks.FormText, blocks.FormNum, blocks.FormBool, blocks.FormChoice}
	for i, f := range b.Form {
		if f.Kind != kinds[i] {
			t.Errorf("%s kind = %q, want %q", f.ID, f.Kind, kinds[i])
		}
	}
	if b.Form[0].Label != "Repository" || !b.Form[0].Required || b.Form[0].Hint != "owner/name" || b.Form[1].Value != "5" || b.Form[2].Value != "true" || len(b.Form[3].Choices) != 2 {
		t.Errorf("form = %+v", b.Form)
	}
}

// Secrets, URL mode, odd field names and too many fields stay on the
// laptop.
func TestFormOfLaptopOnly(t *testing.T) {
	many := make([]ElicitField, blocks.MaxFormFields+1)
	for i := range many {
		many[i] = ElicitField{Name: "f" + strings.Repeat("x", i)}
	}
	cases := map[string]ElicitRequest{
		"password format": formReq(ElicitField{Name: "p", Format: "password"}),
		"token name":      formReq(ElicitField{Name: "api_token"}),
		"pin label":       formReq(ElicitField{Name: "code", Title: "Card PIN"}),
		"url mode":        {Mode: ElicitURL, URL: "https://x"},
		"no fields":       formReq(),
		"bad name":        formReq(ElicitField{Name: "a b"}),
		"too many":        formReq(many...),
	}
	for name, req := range cases {
		if _, ok := FormOf("c", req, nil); ok {
			t.Errorf("%s: the phone may answer", name)
		}
	}
}

// The phone's answers are checked and converted like the laptop's.
func TestFormValues(t *testing.T) {
	req := formReq(
		ElicitField{Name: "repo", Required: true},
		ElicitField{Name: "count", Type: "integer"},
		ElicitField{Name: "draft", Type: "boolean"},
		ElicitField{Name: "color", Enum: []string{"red", "blue"}},
	)
	got, err := FormValues(req, map[string]string{"repo": " a/b ", "count": "3", "draft": "true", "color": "BLUE"})
	if err != nil {
		t.Fatal(err)
	}
	if got["repo"] != "a/b" || got["count"] != 3 || got["draft"] != true || got["color"] != "blue" {
		t.Errorf("values = %v", got)
	}
	got, err = FormValues(req, map[string]string{"repo": "x"})
	if err != nil || len(got) != 1 {
		t.Errorf("optional left out: %v %v", got, err)
	}
	bad := []map[string]string{
		{},                                  // required missing
		{"repo": "x", "count": "three"},     // not a number
		{"repo": "x", "color": "green"},     // not a choice
		{"repo": "x", "other": "1"},         // unknown field
		{"repo": strings.Repeat("x", 2000)}, // too long
	}
	for _, in := range bad {
		if _, err := FormValues(req, in); err == nil {
			t.Errorf("%v: accepted", in)
		}
	}
	if _, err := FormValues(formReq(ElicitField{Name: "password"}), map[string]string{"password": "x"}); err == nil {
		t.Error("a secret field was answered from the phone")
	}
}
