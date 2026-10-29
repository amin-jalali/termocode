package shotkit

import "time"

// Wide-glyph scenes (SCN-11): CJK, an emoji ZWJ sequence, a flag, a
// combining accent and Persian in file names, file text, commit
// subjects and a toast, at the smallest common window.

const (
	wideDoc  = "docs/日本語のとても長いファイル名です.md"
	wideNote = "docs/emoji-👩‍💻-🇮🇷-notes.txt"
)

var wideFiles = map[string]string{
	wideDoc:                      "# 日本語のメモ\n\n中文 👩‍💻 🇮🇷 café سلام دنیا ok\n\n" + "中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文\n",
	wideNote:                     "notes 🇮🇷 👩‍💻\n",
	"docs/café-naïve.txt":       "accents\n",
	"docs/فارسی-and-English.txt": "سلام دنیا\n",
}

// WideScenes are the SCN-11 golden screens.
func WideScenes() []Scene {
	return []Scene{
		// The editor on a CJK-named file: tab, breadcrumbs, explorer.
		{Name: "wide-editor", Files: wideFiles, Steps: []Step{
			Act("view:files"), Act("open:" + wideNote), Act("open:" + wideDoc),
			Act("palette:reveal-file"), Pause(900 * time.Millisecond)}},
		// Source Control: GRAPH with wide commit subjects.
		{Name: "wide-scm", Commits: []string{
			"feat: 中文提交信息 with a long tail",
			"fix: 👩‍💻 pair work 🇮🇷 flag",
			"docs: café naïve",
			"chore: سلام دنیا و English",
		}, Steps: []Step{Act("view:git"), Pause(1500 * time.Millisecond)}},
		// A toast with wide text over the editor.
		{Name: "wide-toast", Files: wideFiles, Steps: []Step{
			Act("open:" + wideDoc), Pause(600 * time.Millisecond),
			Act("toast:保存しました 👩‍💻 🇮🇷|" + wideDoc), Pause(200 * time.Millisecond)}},
	}
}
