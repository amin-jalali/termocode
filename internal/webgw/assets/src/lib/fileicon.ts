// File icons of the desktop workbench: the TUI's table (lib/fileicons.ts,
// generated from internal/theme), looked up the TUI's way: the exact
// name, then a name prefix (LICENSE*, README*, .env.*), then the
// extension; anything else is a plain file.
import { BY_EXT, BY_NAME, BY_PREFIX, DIR, DIR_OPEN, FILE, type IconRow } from "./fileicons";

const byName = new Map(BY_NAME.map((r) => [r[0], r]));
const byExt = new Map(BY_EXT.map((r) => [r[0], r]));

/** fileIcon is the glyph and colour name of a file or folder. */
export function fileIcon(path: string, dir = false, open = false): { glyph: string; color: string } {
  const base = path.slice(path.lastIndexOf("/") + 1).toLowerCase();
  let row: IconRow = FILE;
  if (dir) row = open ? DIR_OPEN : DIR;
  else {
    const exact = byName.get(base);
    const pre = BY_PREFIX.find((r) => base.startsWith(r[0]));
    const dot = base.lastIndexOf(".");
    const ext = dot >= 0 && dot < base.length - 1 ? byExt.get(base.slice(dot + 1)) : undefined;
    row = exact ?? pre ?? ext ?? FILE;
  }
  return { glyph: row[1], color: row[2] };
}
