package ai

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

// ApplyEdit replaces oldText with newText in text (OMX-2). oldText must
// be found exactly once, or with all every place is changed. A text that
// is not found means the model's copy of the file is stale; more than
// one place is ambiguous. A CRLF file takes an LF edit and keeps its
// line ends.
func ApplyEdit(text, oldText, newText string, all bool) (string, error) {
	if oldText == "" {
		return "", errors.New("old_text is empty; to write a whole file use write_file")
	}
	if oldText == newText {
		return "", errors.New("old_text and new_text are the same; nothing to change")
	}
	crlf := strings.Contains(text, "\r\n") && !strings.Contains(oldText, "\r\n")
	if crlf {
		text = strings.ReplaceAll(text, "\r\n", "\n")
	}
	n := strings.Count(text, oldText)
	switch {
	case n == 0:
		return "", errors.New("old_text was not found: the file is not what you think. read the file again and copy the text exactly (spaces and tabs too)")
	case n > 1 && !all:
		return "", fmt.Errorf("old_text matches %d places; add more lines around it so it is unique, or set replace_all", n)
	}
	if all {
		text = strings.ReplaceAll(text, oldText, newText)
	} else {
		text = strings.Replace(text, oldText, newText, 1)
	}
	if crlf {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	return text, nil
}

// EditFileText reads the file at abs and applies the edit of args.
func EditFileText(abs, rel string, args map[string]any) (string, error) {
	if SecretFile(abs) {
		return "", errSecretFile(rel)
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	return ApplyEdit(string(b), ArgString(args, "old_text"), ArgString(args, "new_text"), ArgBool(args, "replace_all"))
}

func lineWord(s string) string {
	if n := strings.Count(s, "\n") + 1; n != 1 {
		return fmt.Sprintf("%d lines", n)
	}
	return "1 line"
}

func editFileTool(w Workspace) Tool {
	return Tool{
		Name: "edit_file", Description: "Change part of a file: replace old_text (copied exactly from the file, unique in it) with new_text. " +
			"Use it instead of write_file for small changes in an existing file. Fails if old_text is not found (read the file again) " +
			"or matches more than one place (add lines around it, or set replace_all).",
		Params: map[string]any{
			"path":        strParam("file path"),
			"old_text":    strParam("the exact text to replace"),
			"new_text":    strParam("the text to put in its place"),
			"replace_all": map[string]any{"type": "boolean", "description": "change every place old_text appears"},
		},
		Required: []string{"path", "old_text", "new_text"}, Mutating: true, Class: ClassFileWrite,
		Validate: w.argsInside("path"),
		Describe: func(a map[string]any) string {
			return fmt.Sprintf("Edit %s (%s → %s)", ArgString(a, "path"), lineWord(ArgString(a, "old_text")), lineWord(ArgString(a, "new_text")))
		},
		Run: func(_ context.Context, a map[string]any) (string, error) {
			abs, err := w.Resolve(ArgString(a, "path"))
			if err != nil {
				return "", err
			}
			text, err := EditFileText(abs, w.rel(abs), a)
			if err != nil {
				return "", err
			}
			if err := os.WriteFile(abs, []byte(text), 0o644); err != nil {
				return "", err
			}
			return "edited " + w.rel(abs), nil
		},
	}
}
