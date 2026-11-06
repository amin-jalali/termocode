// A Markdown file's preview in an editor tab (desktop; the command
// list's Markdown: Open Preview). The laptop reads the open buffer as a
// Markdown tree (buf.markdown), so unsaved changes show too; the page
// builds no markup from strings. It reads again after an edit settles.
import { h } from "preact";
import { useEffect, useState } from "preact/hooks";
import type { BufMarkdown } from "../../../../webproto/proto";
import { laptopBufs } from "../lib/workbench";
import { link, live, store } from "../session";
import { t } from "../strings";
import { Md } from "../ui/Markdown";
import { gitErrText } from "./git/errors";

/** mdBufs are the buffers of the previewed files (the command knows it). */
export const mdBufs = new Map<string, number>();

/** PREVIEW_MS is the pause after an edit before the preview reads again. */
const PREVIEW_MS = 400;

type Load = { state: "loading" } | { state: "ok"; res: BufMarkdown } | { state: "error"; msg: string };

export function MDPreview({ path }: { path: string }) {
  const buf = mdBufs.get(path) ?? laptopBufs.value.find((b) => b.path === path)?.buf;
  const tick = buf === undefined ? -1 : Number((store.topic("buf:" + buf).value?.data as { tick?: unknown } | undefined)?.tick ?? -1);
  const [load, setLoad] = useState<Load>({ state: "loading" });
  const on = live.value;
  useEffect(() => {
    if (buf === undefined || !on) return;
    let stop = false;
    const timer = setTimeout(
      () =>
        link.request("buf.markdown", { buf }).then(
          (r) => !stop && setLoad({ state: "ok", res: r as BufMarkdown }),
          (e: unknown) => !stop && setLoad({ state: "error", msg: gitErrText(e) }),
        ),
      load.state === "loading" ? 0 : PREVIEW_MS,
    );
    return () => {
      stop = true;
      clearTimeout(timer);
    };
  }, [buf, tick, on]);
  return (
    <section class="view md-preview" data-view="md-preview" aria-label={t("md.preview_tab", { name: path })}>
      {buf === undefined ? (
        <p class="md-preview-note">{t("md.open_first")}</p>
      ) : load.state === "loading" ? (
        <p class="md-preview-note" aria-busy="true">{t("md.loading")}</p>
      ) : load.state === "error" ? (
        <p class="md-preview-note">{load.msg}</p>
      ) : (
        <div class="md-preview-body" dir="auto">
          {load.res.md && <Md doc={load.res.md} />}
          {load.res.cut && <p class="md-preview-note">{t("md.cut")}</p>}
        </div>
      )}
    </section>
  );
}
