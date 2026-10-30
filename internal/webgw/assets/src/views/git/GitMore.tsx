// Source control's other git actions (WEBP-5): Commit (Amend) and Clone
// Repository take a line of text here, then ask the laptop (they rewrite
// history or talk to a remote). The More Actions menu lists them with
// the commit graph and the pull requests.
import { h } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import { GRAPH_TAB, PRS_TAB } from "../../lib/gittab";
import { showFile } from "../../lib/workbench";
import { link } from "../../session";
import { t, type StringKey } from "../../strings";
import { AskLaptop } from "../../ui/AskLaptop";
import type { MenuItems } from "../../ui/ContextMenu";
import { Sheet } from "../../ui/Sheet";

export type GitAction = "amend" | "clone";

interface Spec {
  title: StringKey;
  label: StringKey;
  hint: StringKey;
  /** need: the text may not be empty. */
  need: boolean;
  multi: boolean;
  ask(text: string): string;
  send(text: string): Promise<unknown>;
}

const SPECS: Record<GitAction, Spec> = {
  amend: {
    title: "gx.amend",
    label: "gx.amend_label",
    hint: "gx.amend_help_body",
    need: false,
    multi: true,
    ask: (s) => t(s.trim() ? "gx.amend_body_new" : "gx.amend_body_keep"),
    send: (s) => link.request("git.amend", s.trim() ? { message: s.trim() } : {}),
  },
  clone: {
    title: "gx.clone",
    label: "gx.clone_label",
    hint: "gx.clone_help_body",
    need: true,
    multi: false,
    ask: (s) => t("gx.clone_body", { url: s.trim() }),
    send: (s) => link.request("git.clone", { url: s.trim() }),
  },
};

/** GitActionFlow takes the text, then asks the laptop. */
export function GitActionFlow({ kind, onDone }: { kind: GitAction; onDone: () => void }) {
  const spec = SPECS[kind];
  const [text, setText] = useState("");
  const [asking, setAsking] = useState(false);
  const form = useRef<HTMLFormElement | null>(null);
  useEffect(() => {
    const el = form.current?.querySelector<HTMLElement>(".gx-input");
    el?.focus();
  }, [asking]);
  if (asking) return <AskLaptop label={t(spec.title)} text={spec.ask(text)} send={() => spec.send(text)} onDone={onDone} />;
  const ok = !spec.need || text.trim() !== "";
  const onInput = (e: Event) => setText((e.currentTarget as HTMLInputElement).value);
  return (
    <Sheet onLater={onDone} label={t(spec.title)}>
      <form
        ref={form}
        class="ask gx-form"
        onSubmit={(e) => {
          e.preventDefault();
          if (ok) setAsking(true);
        }}
      >
        <h2 class="gx-title">{t(spec.title)}</h2>
        <label class="gx-label">
          {t(spec.label)}
          {spec.multi ? (
            <textarea class="gx-input" dir="auto" rows={3} value={text} maxLength={8000} onInput={onInput} />
          ) : (
            <input class="gx-input" dir="ltr" value={text} maxLength={2048} spellcheck={false} autoComplete="off" onInput={onInput} />
          )}
        </label>
        <p class="ask-note">{t(spec.hint)}</p>
        <div class="choices">
          <button type="button" class="btn btn-neutral" onClick={onDone}>
            {t("cancel")}
          </button>
          <button type="submit" class="btn btn-primary" data-act="next" aria-disabled={ok ? undefined : "true"}>
            {t("gx.next")}
          </button>
        </div>
      </form>
    </Sheet>
  );
}

/** gitMoreMenu is source control's More Actions menu. */
export function gitMoreMenu(repo: boolean, canAct: boolean, op: string, start: (a: GitAction) => void): MenuItems {
  const items: (MenuItems[number])[] = [];
  if (repo) {
    items.push({ label: t("gg.open"), run: () => showFile(GRAPH_TAB) }, { label: t("pr.tab"), run: () => showFile(PRS_TAB) });
    if (canAct) items.push(null, { label: t("gx.amend") + "…", disabled: op === "merge", run: () => start("amend") });
  }
  if (canAct) items.push(null, { label: t("gx.clone") + "…", run: () => start("clone") });
  return items;
}
