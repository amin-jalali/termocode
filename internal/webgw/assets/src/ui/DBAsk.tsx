// What a database read asks the user for, Cancel first: to trust an
// unknown SSH host key (db.trust_host saves exactly the key shown), or a
// secret the laptop does not have (the database's password, the SSH
// password or the SSH key's passphrase), sent once in a masked field to
// stay in the laptop's secret store only; the field is emptied as it is
// sent and the page keeps no copy. The phone's Databases view shows it
// under a failed read; HereAsks (ui/AskLaptop.tsx) shows it in a sheet,
// on the phone and the desktop, when an AI db_* tool waits on the same
// question (the ask ev's conn and need), and its Cancel cancels the tool.
import { h } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import { OfflineError, ReqError } from "../net/link";
import { link } from "../session";
import { t } from "../strings";
import { Button } from "./Button";
import { errText } from "./CardSheet";
import type { DBNeed } from "../views/desk/dbstate";

/** dbErr is the line for a failed database request: the laptop's own
 * reason (the server's words, the password hidden), "Offline" when the
 * link is down, or the generic line for termocode's own faults. */
export function dbErr(e: unknown): string {
  if (e instanceof OfflineError) return t("offline.input");
  if (e instanceof ReqError && e.message && e.code !== "internal") return e.message;
  return errText(e);
}

interface Props {
  conn: string;
  need: DBNeed;
  msg: string;
  onDone: () => void;
  /** onNo is the Cancel (unset: the question stays as its line). */
  onNo?: () => void;
}

export function DBAsk({ conn, need, msg, onDone, onNo }: Props) {
  const [no, setNo] = useState(false);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const safe = useRef<HTMLButtonElement | null>(null);
  const field = useRef<HTMLInputElement | null>(null);
  useEffect(() => (need.need === "host_key" ? safe.current : field.current)?.focus(), [need]);
  if (no) return <p class="empty refused" dir="auto">{msg}</p>;
  const cancel = onNo ?? (() => setNo(true));
  const failed = (e: unknown) => {
    setBusy(false);
    setErr(dbErr(e));
  };
  if (need.need === "host_key") {
    const q = t(need.jump ? "dbv.jump_q" : "dbv.host_q", { host: need.host });
    const trust = () => {
      setBusy(true);
      link.request("db.trust_host", { conn, fingerprint: need.fingerprint }).then(onDone, failed);
    };
    return (
      <div class="row-ask db-need" role="group" aria-label={q}>
        <p class="row-ask-text" dir="auto">
          {q}
        </p>
        <p class="dbn-key" dir="ltr">
          {[need.key_type, need.fingerprint].filter(Boolean).join(" ")}
        </p>
        <p class="view-note">{t("dbv.check_key") + " " + t("dbv.trust_note")}</p>
        {err && <p class="db-note">{err}</p>}
        <div class="choices">
          <button type="button" class="btn btn-neutral" ref={safe} onClick={cancel}>
            {t("cancel")}
          </button>
          <Button kind="primary" needsLink disabled={busy} onClick={trust}>
            {t("dbv.trust")}
          </Button>
        </div>
      </div>
    );
  }
  const kind = need.need;
  const label = kind === "password" ? t("dbv.pw_db") : kind === "ssh_password" ? t("dbv.pw_ssh") : t("dbv.pw_key", { key: need.key ?? "" });
  const send = (e: Event) => {
    e.preventDefault();
    const el = field.current;
    const value = el?.value ?? "";
    if (!el || !value || busy) return;
    el.value = ""; // the page keeps no copy
    setBusy(true);
    link.request("db.secret", { conn, kind, value }).then(onDone, failed);
  };
  return (
    <form class="row-ask db-need" aria-label={label} onSubmit={send}>
      <p class="row-ask-text" dir="auto">
        {msg}
      </p>
      <input ref={field} class="field-box" type="password" aria-label={label} placeholder={label} autocomplete="off" spellcheck={false} autocapitalize="off" />
      <p class="view-note">{t("dbv.pw_note")}</p>
      {err && <p class="db-note">{err}</p>}
      <div class="choices">
        <button type="button" class="btn btn-neutral" onClick={cancel}>
          {t("cancel")}
        </button>
        <button type="submit" class="btn btn-primary" disabled={busy}>
          {t("dbv.pw_send")}
        </button>
      </div>
    </form>
  );
}
