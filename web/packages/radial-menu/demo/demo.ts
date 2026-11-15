// The preview page: the design's own inline menu and the floating button,
// both with the design's sample data. Build: bundle demo.ts to demo.js
// with esbuild (see README).
import { SAMPLE, SAMPLE_FAVORITES, mount, mountMenu } from "../src/index";

let favs = [...SAMPLE_FAVORITES];
try {
  favs = JSON.parse(localStorage.getItem("rm-demo-favs") ?? "null") ?? favs;
} catch {
  /* private mode */
}
const save = (ids: string[]): void => {
  favs = ids;
  try {
    localStorage.setItem("rm-demo-favs", JSON.stringify(ids));
  } catch {
    /* private mode */
  }
};

mountMenu(document.getElementById("inline")!, { categories: SAMPLE, favorites: favs, onFavoritesChange: save });
mount(document.body, { categories: SAMPLE, favorites: favs, onFavoritesChange: save });
