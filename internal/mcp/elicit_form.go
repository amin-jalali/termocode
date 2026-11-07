package mcp

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/amin-jalali/termocode/internal/blocks"
)

// Elicitation as a phone form (MOB-12): a form-mode question whose
// fields are plain (text, number, yes / no, a choice) becomes a form
// block the phone fills in; the answer comes back as strings and
// FormValues checks and converts them like the laptop's prompts. A
// question that asks for something that looks like a password or key,
// a URL-mode question, or one with more fields than a form holds stays
// on the laptop (FormOf says no). termocode never fills anything in.

// MaxFormValue caps one answer from the phone (bytes).
const MaxFormValue = 1000

var formIDOK = regexp.MustCompile(`^[A-Za-z0-9_.:\-]{1,64}$`)

// FormOf is the form block of a form-mode question; ok is false when the
// phone may not answer it (the laptop must).
func FormOf(id string, req ElicitRequest, clean func(string) string) (b blocks.Block, ok bool) {
	if req.Mode != ElicitForm || len(req.Fields) == 0 || len(req.Fields) > blocks.MaxFormFields {
		return blocks.Block{}, false
	}
	m := blockMaker{o: BlockOptions{Clean: clean}}
	server := req.Server
	if server == "" {
		server = "server"
	}
	b = blocks.Block{ID: cutID(id, blocks.MaxID), Kind: blocks.KindForm,
		Title:  m.text("MCP "+server+" asks for input", blocks.MaxTitle),
		ForYou: m.text(req.Message, blocks.MaxForYou)}
	if b.ID == "" {
		b.ID = "form"
	}
	for _, f := range req.Fields {
		if f.Secret() || !formIDOK.MatchString(f.Name) {
			return blocks.Block{}, false
		}
		ff := blocks.BlockFormField{ID: f.Name, Label: m.text(f.Label(), blocks.MaxTitle), Kind: blocks.FormText,
			Required: f.Required, Hint: m.text(f.Description, blocks.MaxSub)}
		if strings.TrimSpace(ff.Label) == "" {
			ff.Label = f.Name
		}
		switch {
		case len(f.Enum) > 0:
			if len(f.Enum) > blocks.MaxChoices {
				return blocks.Block{}, false
			}
			ff.Kind = blocks.FormChoice
			for _, e := range f.Enum {
				c := m.text(e, blocks.MaxTitle)
				if c != e { // a choice must go back exactly as the server sent it
					return blocks.Block{}, false
				}
				ff.Choices = append(ff.Choices, c)
			}
		case f.Type == "boolean":
			ff.Kind = blocks.FormBool
		case f.Type == "integer" || f.Type == "number":
			ff.Kind = blocks.FormNum
		}
		if f.Default != nil {
			ff.Value = m.text(scalarText(f.Default), blocks.MaxSub)
			if ff.Kind == blocks.FormBool {
				ff.Value = strconv.FormatBool(f.Default == true)
			}
		}
		b.Form = append(b.Form, ff)
	}
	if b.Validate() != nil { // a label the blocks rules call secret, too long …
		return blocks.Block{}, false
	}
	return b, true
}

// FormValues checks the phone's answers against the question and
// converts them (a number, yes / no, one of the choices). An empty
// optional field is left out; an unknown field, a missing required one
// or a secret-looking field is an error.
func FormValues(req ElicitRequest, in map[string]string) (map[string]any, error) {
	known := map[string]bool{}
	for _, f := range req.Fields {
		known[f.Name] = true
	}
	for k, v := range in {
		if !known[k] {
			return nil, fmt.Errorf("unknown field %q", k)
		}
		if len(v) > MaxFormValue || !utf8.ValidString(v) {
			return nil, fmt.Errorf("the answer for %q is too long", k)
		}
	}
	out := map[string]any{}
	for _, f := range req.Fields {
		if f.Secret() {
			return nil, fmt.Errorf("%s: answer this on the laptop", f.Label())
		}
		v := in[f.Name]
		if f.Type == "boolean" && len(f.Enum) == 0 && v == "" && !f.Required {
			continue
		}
		val, set, err := ElicitValue(f, v)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.Label(), err)
		}
		if set {
			out[f.Name] = val
		}
	}
	return out, nil
}

// ElicitValue checks and converts one typed value ("" skips an optional
// field).
func ElicitValue(f ElicitField, v string) (any, bool, error) {
	if !f.Secret() {
		v = strings.TrimSpace(v)
	}
	if v == "" {
		if f.Required {
			return nil, false, fmt.Errorf("%s is required", f.Label())
		}
		return nil, false, nil
	}
	if len(f.Enum) > 0 {
		for _, e := range f.Enum {
			if strings.EqualFold(e, v) {
				return e, true, nil
			}
		}
		return nil, false, fmt.Errorf("one of: %s", strings.Join(f.Enum, ", "))
	}
	switch f.Type {
	case "integer":
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, false, fmt.Errorf("a whole number, please")
		}
		return n, true, nil
	case "number":
		n, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil, false, fmt.Errorf("a number, please")
		}
		return n, true, nil
	case "boolean":
		switch strings.ToLower(v) {
		case "y", "yes", "true", "1":
			return true, true, nil
		case "n", "no", "false", "0":
			return false, true, nil
		}
		return nil, false, fmt.Errorf("yes or no, please")
	}
	return v, true, nil
}
