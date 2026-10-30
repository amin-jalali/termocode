// The commit box (web UX 3.5): at the bottom, in the thumb zone. A
// one-line subject with a counter (amber from 50, red from 72), an
// optional body, and Commit. Commit asks inside the box, not in a modal:
// "Commit 2 staged files to main?" with Cancel first and focused. No
// amend from the web.
import { h } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import { link } from "../../session";
import { toast } from "../../lib/toasts";
import { commitMessage } from "../../state/git";
import { Button } from "../../ui/Button";
import { t, tn } from "../../strings";
import { gitFailed } from "./errors";

const WARN_AT = 50;
const MAX_AT = 72;

export function CommitBox({ staged, branch }: { staged: number; branch: string }) {
  const [subject, setSubject] = useState("");
  const [body, setBody] = useState("");
  const [more, setMore] = useState(false);
  const [asking, setAsking] = useState(false);
  const [busy, setBusy] = useState(false);
  const cancel = useRef<HTMLButtonElement | null>(null);

  useEffect(() => {
    if (asking) cancel.current?.focus();
  }, [asking]);
  useEffect(() => {
    if (staged === 0) setAsking(false);
  }, [staged]);

  const n = subject.length;
  const ready = subject.trim() !== "" && staged > 0 && !busy;
  const commit = () => {
    setBusy(true);
    link
      .request("git.commit", { message: commitMessage(subject, body) })
      .then((r) => {
        const d = (r ?? {}) as { hash?: unknown; files?: unknown };
        toast("ok", t("git.committed", { hash: String(d.hash ?? ""), n: Number(d.files ?? staged) }));
        setSubject("");
        setBody("");
        setMore(false);
      }, gitFailed)
      .finally(() => {
        setBusy(false);
        setAsking(false);
      });
  };

  return (
    <form class="commit-box" onSubmit={(e) => e.preventDefault()}>
      <div class="commit-line">
        <input
          class="commit-subject"
          dir="auto"
          value={subject}
          maxLength={1000}
          aria-label={t("git.subject")}
          placeholder={t("git.subject")}
          onInput={(e) => setSubject((e.currentTarget as HTMLInputElement).value.replace(/[\r\n]/g, " "))}
        />
        <span class={`commit-count${n >= MAX_AT ? " is-max" : n >= WARN_AT ? " is-warn" : ""}`} aria-hidden="true">
          {n}/{MAX_AT}
        </span>
      </div>
      {more ? (
        <textarea
          class="commit-body"
          dir="auto"
          rows={3}
          value={body}
          aria-label={t("git.body")}
          placeholder={t("git.body")}
          onInput={(e) => setBody((e.currentTarget as HTMLTextAreaElement).value)}
        />
      ) : (
        <button type="button" class="commit-more" onClick={() => setMore(true)}>
          {t("git.body")}
        </button>
      )}
      {asking ? (
        <div class="commit-ask" role="group" aria-label={t("git.commit_do")}>
          <p class="commit-ask-text">{t("git.commit_confirm", { n: staged, branch })}</p>
          <div class="choices">
            <button type="button" class="btn btn-neutral" ref={cancel} onClick={() => setAsking(false)}>
              {t("cancel")}
            </button>
            <Button kind="primary" needsLink disabled={busy} onClick={commit}>
              {t("git.commit_do")}
            </Button>
          </div>
        </div>
      ) : (
        <Button kind="primary" wide needsLink disabled={!ready} onClick={() => setAsking(true)}>
          {tn("git.commit", staged)}
        </Button>
      )}
    </form>
  );
}
