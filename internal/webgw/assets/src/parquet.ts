// Export as Parquet of the desktop's database grid (parquet.js): the
// workbench loads this file only when the entry is used (lib/parquet.ts), so
// its own budget does not grow. It imports no value of the page: the link,
// the download and the toast come from the host. The laptop writes the file
// (db.export_parquet) from the rows the grid shows, masked values still
// masked; the strings are English only, like the other chunks.
import type { DBRowsView } from "./state/more";

/** ParquetHost is what the workbench gives this file. */
export interface ParquetHost {
  link: { request(op: string, args?: unknown): Promise<unknown> };
  saveBlob(b: Blob, name: string): void;
  toast(severity: "warn", title: string): number;
}

// One request holds 1 MiB with its envelope.
const MAX_ARGS = (1 << 20) - 4096;

/** save asks the laptop for the Parquet file of r and hands it to the browser as name.parquet. */
export async function save(host: ParquetHost, r: DBRowsView, name: string): Promise<void> {
  const args = JSON.stringify({ columns: r.columns, rows: r.rows.map((row) => row.map((c) => (c.n ? null : c.v))) });
  if (args.length > MAX_ARGS) return void host.toast("warn", "Too many values for one Parquet file: narrow the result first.");
  try {
    const res = (await host.link.request("db.export_parquet", JSON.parse(args))) as { data?: string };
    const bin = atob(res.data ?? "");
    const bytes = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
    host.saveBlob(new Blob([bytes], { type: "application/vnd.apache.parquet" }), name + ".parquet");
  } catch {
    host.toast("warn", "Could not write the Parquet file.");
  }
}
