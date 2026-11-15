// The commit sheet (page design 06): Commit opens it over the changes.
// ✕ and "Commit changes" on top; one box for the message, its first
// line the subject and an optional description under it; the staged
// files in one line; "✦ Write it for me" (the laptop's AI writes the
// message from the staged diff, git.ai_message; then "✦ Rewrite"), the
// branch (it opens the branches) and a green Commit side by side. The
// sheet itself is the question: Commit commits to the branch it names.
import type { GitState } from "../state/git";
import { splitPath } from "../state/git";
import { H, h } from "./jsx";
import { s } from "./strings";
import { Icon } from "./ui";

export function CommitSheet({ g, msg, setMsg, onClose, onDone }: { g: GitState; msg: string; setMsg: (m: string) => void; onClose: () => void; onDone: () => void }) {
  const [busy, setBusy] = H.useState<"" | "ai" | "commit">("");
  const box = H.useRef<HTMLTextAreaElement | null>(null);
  const staged = g.files.filter((f) => f.staged);
  const names = staged.map((f) => splitPath(f.path).name);
  const shown = names.length > 3 ? names.slice(0, 3).join(", ") + " " + s("and_more", { n: names.length - 3 }) : names.join(", ");
  H.useEffect(() => {
    const el = box.current;
    if (el && !msg) el.focus();
  }, []);
  // The box grows with its text, up to half the screen.
  H.useEffect(() => {
    const el = box.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = Math.min(el.scrollHeight, innerHeight * 0.4) + "px";
  }, [msg]);

  const write = () => {
    setBusy("ai");
    H.link
      .request("git.ai_message", {})
      .then((r) => {
        const m = String((r as { message?: unknown } | null)?.message ?? "");
        if (m) setMsg(m);
      }, H.failed)
      .finally(() => setBusy(""));
  };
  const commit = () => {
    if (!msg.trim()) {
      H.toast("warn", s("msg_first"));
      box.current?.focus();
      return;
    }
    setBusy("commit");
    H.link
      .request("git.commit", { message: cleanMessage(msg) })
      .then((r) => {
        const d = (r ?? {}) as { hash?: unknown; files?: unknown };
        H.toast("ok", H.t("git.committed", { hash: String(d.hash ?? ""), n: Number(d.files ?? staged.length) }));
        setMsg("");
        onDone();
      }, H.failed)
      .finally(() => setBusy(""));
  };

  return (
    <H.Sheet onLater={onClose} label={s("commit_title")}>
      <div class="gp-cs">
        <div class="gp-cs-h">
          <button type="button" class="gp-ib gp-x" data-act="close" aria-label={H.t("cancel")} onClick={onClose}>
            <Icon n="close" size={22} />
          </button>
          <h2 class="gp-cs-t">{s("commit_title")}</h2>
        </div>
        <textarea
          ref={box}
          class="gp-cs-msg"
          dir="auto"
          rows={3}
          maxLength={20000}
          value={msg}
          aria-label={H.t("git.subject")}
          placeholder={s("msg_ph")}
          onInput={(e) => setMsg((e.currentTarget as HTMLTextAreaElement).value)}
        />
        <p class="gp-cs-files">{staged.length ? s(staged.length === 1 ? "staged_one" : "staged_files", { n: staged.length, names: shown }) : s("stage_first")}</p>
        <div class="gp-cs-acts">
          <button type="button" class="gp-cs-ai" data-act="write" disabled={busy !== "" || staged.length === 0} aria-busy={busy === "ai" ? "true" : "false"} onClick={write}>
            <Icon n="spark" size={15} />
            {busy === "ai" ? s("writing") : s(msg.trim() ? "rewrite" : "write")}
          </button>
          <span class="gp-sp" />
          <button type="button" class="gp-cs-br" data-act="branch" aria-label={s("branch", { name: g.branch.name })} onClick={() => H.go({ tab: "git", rest: "", query: { sheet: "remote" } })}>
            <Icon n="branch" size={15} />
            <span class="gp-pill-n" dir="ltr">
              {g.branch.name}
            </span>
            <Icon n="down" size={14} />
          </button>
          <button type="button" class="gp-cs-go" data-act="commit-do" disabled={busy !== "" || staged.length === 0} onClick={commit}>
            <Icon n="check" size={16} />
            {H.t("git.commit_do")}
          </button>
        </div>
      </div>
    </H.Sheet>
  );
}

/** cleanMessage is the message as git wants it: the subject trimmed,
 * then a blank line and the description when there is one. */
export function cleanMessage(m: string): string {
  const lines = m.replace(/\r/g, "").split("\n");
  const subject = (lines.shift() ?? "").trim();
  const body = lines.join("\n").trim();
  return body ? `${subject}\n\n${body}` : subject;
}
