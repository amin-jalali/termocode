// The desktop editor's context menu (a right click in the editor's
// frame): the page draws it like the other menus. Cut, Copy and Paste go
// through this device's clipboard (the frame cannot reach it); Go to
// Definition, Find All References and Format run the editor's own
// actions when it has them (Format shows only then). The page never reads
// the clipboard (web design 3.4): Paste gives the editor the keyboard and
// says which keys paste.
import type { FromFrame, ToFrame } from "../monaco/protocol";
import { t } from "../strings";
import { openMenu, type MenuItems } from "../ui/ContextMenu";
import { modKey } from "./platform";
import { toast } from "./toasts";
import { quick } from "./workbench";

type MenuMsg = Extract<FromFrame, { t: "menu" }>;
/** editorMenu opens the menu of a right click at (x, y) on the page. */
export function editorMenu(x: number, y: number, m: MenuMsg, edit: (e: ToFrame) => void): void {
  const mod = modKey();
  const fail = () => toast("warn", t("copy.failed"));
  const write = (then?: () => void) => {
    try {
      navigator.clipboard.writeText(m.sel).then(then, fail);
    } catch {
      fail();
    }
  };
  const items: (MenuItems[number])[] = [
    { label: t("menu.def"), keys: "F12", disabled: !m.can.def, run: () => edit({ t: "edit", op: "def" }) },
    { label: t("menu.refs"), keys: "Shift+F12", disabled: !m.can.refs, run: () => edit({ t: "edit", op: "refs" }) },
    null,
    { label: t("menu.cut"), keys: `${mod}+X`, disabled: m.readOnly || !m.sel, run: () => write(() => edit({ t: "edit", op: "cut" })) },
    { label: t("copy"), keys: `${mod}+C`, disabled: !m.sel, run: () => write() },
    {
      label: t("key.paste"),
      keys: `${mod}+V`,
      disabled: m.readOnly,
      run: () => {
        edit({ t: "focus" });
        toast("info", t("menu.paste_keys", { keys: `${mod}+V` }));
      },
    },
  ];
  if (m.can.format) items.push(null, { label: t("menu.format"), keys: "Shift+Alt+F", run: () => edit({ t: "edit", op: "format" }) });
  items.push(null, { label: t("menu.palette"), keys: `${mod}+Shift+P`, run: () => (quick.value = { mode: "commands" }) });
  openMenu(x, y, t("menu.editor"), items);
}
