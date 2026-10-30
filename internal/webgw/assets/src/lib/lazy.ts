// Loads the in-page editor (WEB-39) on first use: editor.js is built
// beside app.js (cmd/webbuild) and is the page's only dynamic import, a
// fixed same-origin path (lint_test.go allows exactly this one). A
// failed load can be tried again.
import type { EditorModule } from "../editor/api";

let loading: Promise<EditorModule> | null = null;

export function loadEditor(): Promise<EditorModule> {
  if (!loading) {
    loading = import("/assets/editor.js").catch((e: unknown) => {
      loading = null;
      throw e;
    });
  }
  return loading;
}
