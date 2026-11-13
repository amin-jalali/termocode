// The session picker (WEB-55): after a passkey sign-in the page lists the
// owner's running termocode sessions on this laptop (name, branch, last
// activity) and opens the one they tap. A single running session needs
// no choice, so the step is skipped. It only picks: it never starts a
// session, and every pick keeps the passkey login of this device.
import { h } from "preact";
import { useEffect } from "preact/hooks";
import { ago, pickerRows, type SessionRow } from "../lib/sessions";
import { link, store } from "../session";
import { t } from "../strings";

interface Props {
  /** onPick opens the workspace; row is the picked session, undefined for this one. */
  onPick: (row?: SessionRow) => void;
}

function When({ ms }: { ms: number }) {
  const a = ago(ms, Date.now());
  if (a.key === "") return null;
  return <span class="sp-when">{t(a.key, { n: a.n })}</span>;
}

function Row({ r, onPick }: { r: SessionRow; onPick: Props["onPick"] }) {
  return (
    <li>
      <button type="button" class={`sp-row${r.here ? " is-here" : ""}`} data-key={r.id} onClick={() => onPick(r.here ? undefined : r)}>
        <span class="sp-main">
          <span class="sp-name" dir="auto">
            {r.name}
            {r.here && <span class="sp-tag">{t("sp.here")}</span>}
          </span>
          {r.where && (
            <span class="sp-where" dir="ltr">
              {r.where}
            </span>
          )}
          <span class="sp-facts">
            {r.branch && (
              <span class="sp-branch" dir="ltr">
                ⎇ {r.branch}
              </span>
            )}
            {r.waiting > 0 && <span class="sp-need">{t("sp.waiting", { n: r.waiting })}</span>}
            {r.working > 0 && <span>{t("sp.working", { n: r.working })}</span>}
            <When ms={r.openedAt} />
          </span>
        </span>
        <span class="sp-go" aria-hidden="true">
          ›
        </span>
      </button>
    </li>
  );
}

export function SessionPicker({ onPick }: Props) {
  useEffect(() => link.subscribe("projects"), []);
  const topic = store.topic("projects").value;
  const rows = topic ? pickerRows(topic.data) : null;
  const ready = !!rows && (topic?.data as { state?: string } | undefined)?.state === "ready";
  // One running session (this one) is no choice: go straight in.
  useEffect(() => {
    if (ready && rows && rows.length <= 1) onPick(undefined);
  }, [ready, rows?.length]);
  return (
    <main class="connect sp" id="main" tabIndex={-1}>
      <div class="connect-top" />
      <div class="sp-box">
        <h1 class="sp-title">{t("sp.title")}</h1>
        <p class="connect-line">{t("sp.body")}</p>
        {!ready || !rows || rows.length <= 1 ? (
          <p class="connect-line" role="status">
            {t("sp.loading")}
          </p>
        ) : (
          <ul class="sp-list" aria-label={t("sp.title")}>
            {rows.map((r) => (
              <Row key={r.id} r={r} onPick={onPick} />
            ))}
          </ul>
        )}
        <button type="button" class="btn btn-neutral sp-skip" onClick={() => onPick(undefined)}>
          {t("sp.continue")}
        </button>
      </div>
    </main>
  );
}
