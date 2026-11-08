// What a failed database read asks the user for, inline under its
// connection in the Databases view: an unknown SSH host key, shown with
// its host, type and fingerprint (Cancel first and focused). Trust and
// save asks the laptop to add exactly that key to its known_hosts; the
// view then reads again. Esc and Enter never trust.
import { h } from "preact";
import { useLayoutEffect, useRef, useState } from "preact/hooks";
import { link } from "../../session";
import { t } from "../../strings";
import { dbErr, type DBNeed } from "./dbstate";

export function DBNeedAsk({ conn, need, onDone, onNo }: { conn: string; need: DBNeed; onDone: () => void; onNo: () => void }) {
  const safe = useRef<HTMLButtonElement | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  useLayoutEffect(() => safe.current?.focus(), []);
  const q = t(need.jump ? "dbv.jump_q" : "dbv.host_q", { host: need.host });
  const trust = () => {
    setBusy(true);
    setErr("");
    link.request("db.trust_host", { conn, fingerprint: need.fingerprint }).then(onDone, (e: unknown) => {
      setBusy(false);
      setErr(dbErr(e));
    });
  };
  return (
    <div class="pv-ask db-need" role="group" aria-label={q} onKeyDown={(e) => e.key === "Escape" && (e.stopPropagation(), onNo())}>
      <p>{q}</p>
      <p class="db-need-key">
        <code>{[need.key_type, need.fingerprint].filter(Boolean).join(" ")}</code>
      </p>
      <p class="db-need-note">{t("dbv.check_key") + " " + t("dbv.trust_note")}</p>
      {err && <p class="c-err">{err}</p>}
      <div class="pv-ask-row">
        <button type="button" class="pv-btn is-2" ref={safe} onClick={onNo}>
          {t("cancel")}
        </button>
        <button type="button" class="pv-btn" disabled={busy} onClick={trust} onKeyDown={(e) => e.key === "Enter" && e.preventDefault()}>
          {t("dbv.trust")}
        </button>
      </div>
    </div>
  );
}
