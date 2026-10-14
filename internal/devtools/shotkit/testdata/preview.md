# Markdown Preview

A small sample that uses **every common block**, so the reader view can be
checked by eye. It has *emphasis*, **strong text**, ~~old words~~, `inline code`
and a [link to the docs](https://example.com/docs/preview). Long paragraphs
should wrap at word boundaries inside a centred reading column instead of
running across the whole window, which is very hard to read on a wide screen.

## Getting started

Install the tool, open a `.md` file and run **Markdown: Preview**:

1. Open the command palette with `F1`.
2. Type `preview` and press Enter. This item is long on purpose so that the
   wrapped line shows a clean hanging indent under the text, not the number.
3. Scroll with `j` / `k`, jump with `g` / `G`.

### Lists

- Fast start, no config needed
- Works in light and dark themes
  - Nested items use a different bullet
    - And a third level too
- Plain text stays plain: done, open and TODO are just words

### Tasks

- [x] Centre the reading column
- [x] Draw clean headings
- [ ] Ship it

#### Code

```go
// Greet says hello.
func Greet(name string) string {
	if name == "" {
		name = "world"
	}
	return fmt.Sprintf("Hello, %s!", name) // a comment at the end of a long line that will not fit
}
```

> **Note:** quotes get a coloured bar on the left. They can span more than
> one line and wrap nicely too.

---

## Table

| Key     | Action          | Mode   |
|:--------|:----------------|-------:|
| `j`     | Scroll down     | normal |
| `k`     | Scroll up       | normal |
| `g`/`G` | Top / bottom    | normal |
| `esc`   | Close preview   | any    |

That's all. See the [guide](guide.md) for more.
