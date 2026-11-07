// The user's own mappings for the desktop editor's Vim mode (WEBP-1):
// the laptop editor's, asked once (editor.vim_maps) the first time Vim
// mode is on; a failed ask is tried again the next time.
import type { VimMapList } from "../../../../webproto/proto";
import { link } from "../session";

let asked: Promise<VimMapList | null> | null = null;

export function vimMaps(): Promise<VimMapList | null> {
  if (!asked) {
    asked = link.request("editor.vim_maps").then(
      (r) => r as VimMapList,
      () => {
        asked = null;
        return null;
      },
    );
  }
  return asked;
}
