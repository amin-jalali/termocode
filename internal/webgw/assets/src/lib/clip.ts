// Copy Path and Copy Relative Path (the context menus): the text goes to
// the system clipboard of this device, and a toast says if it did.
import { status } from "../session";
import { t } from "../strings";
import { toast } from "./toasts";

/** copyText puts text on the clipboard (it must run in a click). */
export function copyText(text: string): void {
  const fail = () => toast("warn", t("copy.failed"));
  try {
    navigator.clipboard.writeText(text).then(() => toast("ok", t("copied")), fail);
  } catch {
    fail();
  }
}

/** absPath is a workspace path as the laptop names it (its root first,
 * with the laptop's own separator). */
export function absPath(rel: string): string {
  const root = status.peek().root;
  if (!root) return rel;
  const win = root.includes("\\") && !root.includes("/");
  const sep = win ? "\\" : "/";
  const tail = win ? rel.replaceAll("/", "\\") : rel;
  if (!tail) return root;
  return root.endsWith(sep) ? root + tail : root + sep + tail;
}
