// Two workspace files side by side (desktop; File: Compare Two Files…),
// both as they are on disk (file.text), in a read-only diff editor.
import { h } from "preact";
import { useEffect, useState } from "preact/hooks";
import type { FileText } from "../../../../webproto/proto";
import { link } from "../session";
import { t } from "../strings";
import { Sides } from "./AIDiff";
import { gitErrText } from "./git/errors";
import { View } from "./View";
import { Chip } from "../ui/Chip";

type Load = { state: "loading" } | { state: "ok"; a: FileText; b: FileText } | { state: "error"; msg: string };

const nameOf = (p: string) => p.slice(p.lastIndexOf("/") + 1);

export function CompareView({ a, b, slot }: { a: string; b: string; slot: string }) {
  const [load, setLoad] = useState<Load>({ state: "loading" });
  useEffect(() => {
    let stop = false;
    Promise.all([link.request("file.text", { path: a }), link.request("file.text", { path: b })]).then(
      ([x, y]) => !stop && setLoad({ state: "ok", a: x as FileText, b: y as FileText }),
      (e: unknown) => !stop && setLoad({ state: "error", msg: gitErrText(e) }),
    );
    return () => {
      stop = true;
    };
  }, [a, b]);
  if (load.state === "loading") return <p class="md-preview-note" aria-busy="true">{t("md.loading")}</p>;
  if (load.state === "error") return <p class="empty refused">{load.msg}</p>;
  const bin = load.a.binary ? a : load.b.binary ? b : "";
  if (bin) return <p class="empty refused">{t("cmp.binary", { name: nameOf(bin) })}</p>;
  return (
    <View tab="files" name="compare" title={nameOf(a) + " ↔ " + nameOf(b)} bodyClass="is-editing" actions={<Chip>{t("viewonly")}</Chip>}>
      {(load.a.truncated || load.b.truncated) && (
        <p class="file-note" role="status">
          {t("cmp.cut")}
        </p>
      )}
      <div class="diff-host">
        <Sides path={b} left={load.a.text.replace(/\n$/, "")} right={load.b.text.replace(/\n$/, "")} slot={slot} readOnly revert={false} />
      </div>
    </View>
  );
}
