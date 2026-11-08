// What a failed database read asks the user for, inline under its
// connection in the Databases view (Cancel first; Esc cancels, Enter
// never trusts):
//   - an unknown SSH host key, shown with its host, type and fingerprint:
//     Trust and save asks the laptop to add exactly that key to its
//     known_hosts;
//   - a secret the laptop does not have (the database's password, the SSH
//     password or the SSH key's passphrase): a masked field whose value is
//     sent once, to stay in the laptop's secret store only. The page keeps
//     no copy: the field is emptied as it is sent.
// Then the view reads again.
import { h } from "preact";
import { useLayoutEffect, useRef, useState } from "preact/hooks";
import { link } from "../../session";
import { t } from "../../strings";
import { dbErr, type DBNeed, type DBSecretKind } from "./dbstate";

interface Props {
  conn: string;
  need: DBNeed;
  msg: string;
  onDone: () => void;
  onNo: () => void;
}

export function DBNeedAsk(p: Props) {
  return p.need.need === "host_key" ? <HostAsk {...p} /> : <SecretAsk {...p} />;
}

function HostAsk({ conn, need, onDone, onNo }: Props) {
  const safe = useRef<HTMLButtonElement | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  useLayoutEffect(() => safe.current?.focus(), []);
  if (need.need !== "host_key") return null;
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

function secretLabel(kind: DBSecretKind, key: string | undefined): string {
  if (kind === "password") return t("dbv.pw_db");
  if (kind === "ssh_password") return t("dbv.pw_ssh");
  return t("dbv.pw_key", { key: key ?? "" });
}

function SecretAsk({ conn, need, msg, onDone, onNo }: Props) {
  const field = useRef<HTMLInputElement | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  useLayoutEffect(() => field.current?.focus(), []);
  if (need.need === "host_key") return null;
  const kind = need.need;
  const label = secretLabel(kind, need.key);
  const send = (e: Event) => {
    e.preventDefault();
    const el = field.current;
    const value = el?.value ?? "";
    if (!el || !value || busy) return;
    el.value = ""; // the page keeps no copy
    setBusy(true);
    setErr("");
    link.request("db.secret", { conn, kind, value }).then(onDone, (x: unknown) => {
      setBusy(false);
      setErr(dbErr(x));
      field.current?.focus();
    });
  };
  const id = `dbn-${conn}`;
  return (
    <form class="pv-ask db-need" aria-label={label} onSubmit={send} onKeyDown={(e) => e.key === "Escape" && (e.stopPropagation(), onNo())}>
      <p>{msg}</p>
      <label class="db-need-field" for={id}>
        {label}
      </label>
      <div class="pv-input">
        <input id={id} ref={field} type="password" autocomplete="off" spellcheck={false} autocapitalize="off" />
      </div>
      <p class="db-need-note">{t("dbv.pw_note")}</p>
      {err && <p class="c-err">{err}</p>}
      <div class="pv-ask-row">
        <button type="button" class="pv-btn is-2" onClick={onNo}>
          {t("cancel")}
        </button>
        <button type="submit" class="pv-btn" disabled={busy}>
          {t("dbv.pw_send")}
        </button>
      </div>
    </form>
  );
}
