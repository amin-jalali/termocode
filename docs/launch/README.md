# Launch drafts (v0.1.0)

Drafts only. Nothing here is posted, tagged or sent. The owner decides
when and where (release tag and public posts are owner decisions).

| File | For |
|---|---|
| [hn.md](hn.md) | Show HN title options, first comment, answers to likely questions |
| [reddit-neovim.md](reddit-neovim.md) | r/neovim post, plus short r/commandline, r/golang and r/LocalLLaMA variants |
| [lists.md](lists.md) | awesome lists, newsletters, directories: the one-paragraph entries |
| [persian.md](persian.md) | Persian community: Virgool / LinkedIn / Telegram texts |
| [CHANGELOG.md](https://github.com/amin-jalali/termocode/blob/main/CHANGELOG.md) | what is in v0.1.0 |

## Rules for every text

- Use only what is built and in the docs today. If a line is not true on
  a clean machine, cut it.
- Headline: **The AI-Integrated Terminal IDE.** Pillars: Full-Featured.
  AI-Integrated. Persistent. Tagline: **Your Connection Dies. Your Work
  Lives On.**
- Do not compare with other products by name and do not say "like X".
- Numbers only from [benchmarks.md](../benchmarks.md) (about 60 MB of RAM
  with its editor engine, near-zero CPU when idle, under 70 ms start).
- Do not promise Windows. Today: Linux, macOS, Android (Termux).
- Say honestly what is rough: the web panel is experimental; the first
  start downloads three editor plugins.

## Before anything is posted (owner steps)

1. Tag `v0.1.0` and let `release.yml` build the binaries.
2. Test the one-line install on a clean Ubuntu container and on macOS:
   `termocode --version`, first start with no network, `termocode setup`.
3. Check every command and number in the drafts against that build.
4. Record or pick the demo GIF (`assets/gifs/hero.gif`).
5. Pick a time: Tuesday to Thursday, about 8:00 to 10:00 Eastern.
   Stay online for four hours and answer every comment.
