// Export as Parquet (desktop) lives in parquet.js: the workbench's one
// dynamic import for it loads the file on first use and gives it the link,
// the download and the toast. A failed load can be tried again.
import type * as ParquetModule from "../parquet";
import { link } from "../session";
import { t } from "../strings";
import type { DBRowsView } from "../state/more";
import { toast } from "./toasts";
import { saveBlob } from "./transfer";

let mod: Promise<typeof ParquetModule> | null = null;

/** saveParquet writes r (what the grid shows) as name.parquet. */
export function saveParquet(r: DBRowsView, name: string): void {
  (mod ??= import("/assets/parquet.js"))
    .then((m) => m.save({ link, saveBlob, toast }, r, name))
    .catch(() => ((mod = null), toast("warn", t("cmd.failed"))));
}
