// What the page may do to a file or folder (WEB-41, web design 2.3): new
// file and new folder in a folder, rename, delete. A name is typed in the
// sheet, which is the in-page confirm (Cancel first and focused; Enter in
// the field never acts while the name is not valid). Delete asks the
// laptop. The hub checks every path again.
import { h } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import { toast } from "../../lib/toasts";
import { link, live } from "../../session";
import { t } from "../../strings";
import { AskLaptop } from "../../ui/AskLaptop";
import { Button } from "../../ui/Button";
import { Sheet } from "../../ui/Sheet";
import { gitFailed as failed } from "../git/errors";
import { DownloadSheet, UploadSheet } from "./Transfer";

export interface Target {
  /** path is the workspace path ("" for the workspace folder). */
  path: string;
  dir: boolean;
  /** start skips the menu (a context menu chose the action). */
  start?: "new-file" | "new-dir" | "rename" | "delete";
}

type Step = { s: "menu" } | { s: "new"; dir: boolean } | { s: "rename" } | { s: "delete" } | { s: "download" } | { s: "upload" };

const base = (p: string): string => p.slice(p.lastIndexOf("/") + 1);
const join = (dir: string, name: string): string => (dir ? dir + "/" + name : name);

/** goodName is one plain name, as the hub's wire rule wants it. */
export function goodName(n: string): boolean {
  return n !== "" && n !== "." && n !== ".." && !/[/\\\u0000-\u001f\u007f]/.test(n) && new TextEncoder().encode(n).length <= 255;
}

function NameForm({ label, start, verb, onSend, onCancel }: { label: string; start: string; verb: string; onSend: (name: string) => void; onCancel: () => void }) {
  const [name, setName] = useState(start);
  const input = useRef<HTMLInputElement | null>(null);
  useEffect(() => {
    const el = input.current;
    if (!el) return;
    el.focus();
    const dot = start.lastIndexOf(".");
    el.setSelectionRange(0, dot > 0 ? dot : start.length);
  }, []);
  const ok = goodName(name.trim()) && name.trim() !== start;
  return (
    <form
      class="name-form"
      onSubmit={(e) => {
        e.preventDefault();
        if (ok && live.value) onSend(name.trim());
      }}
    >
      <label class="field-label" for="name-input">
        {label}
      </label>
      <input
        id="name-input"
        ref={input}
        class="search-input name-input"
        dir="auto"
        autocomplete="off"
        autocapitalize="off"
        spellcheck={false}
        value={name}
        onInput={(e) => setName((e.currentTarget as HTMLInputElement).value)}
      />
      {name.trim() !== "" && !goodName(name.trim()) && <p class="name-bad">{t("files.bad_name")}</p>}
      <div class="choices">
        <button type="button" class="btn btn-neutral" onClick={onCancel}>
          {t("cancel")}
        </button>
        <Button kind="primary" needsLink disabled={!ok} onClick={() => onSend(name.trim())}>
          {verb}
        </Button>
      </div>
    </form>
  );
}

export function FileActions({ target, onDone }: { target: Target; onDone: () => void }) {
  const first: Step =
    target.start === "new-file" ? { s: "new", dir: false } : target.start === "new-dir" ? { s: "new", dir: true } : target.start === "rename" ? { s: "rename" } : target.start === "delete" ? { s: "delete" } : { s: "menu" };
  const [step, setStep] = useState<Step>(first);
  const where = target.path || t("files.ws_root");
  const run = (op: string, args: Record<string, string>, done: (path: string) => void) => {
    link.request(op, args).then((r) => {
      const p = String((r as { path?: unknown } | null)?.path ?? "");
      done(p);
      onDone();
    }, failed);
  };

  if (step.s === "download") return <DownloadSheet path={target.path} onDone={onDone} />;
  if (step.s === "upload") return <UploadSheet dir={target.path} onDone={onDone} />;
  if (step.s === "delete") {
    return (
      <AskLaptop
        label={t("files.delete")}
        text={t(target.dir ? "files.delete_dir_body" : "files.delete_body", { path: target.path })}
        send={() => link.request("tree.delete", { path: target.path })}
        onDone={onDone}
      />
    );
  }
  return (
    <Sheet onLater={onDone} label={t("files.actions", { name: base(where) })}>
      <div class="file-actions">
        <p class="file-actions-path" dir="auto">
          {where}
        </p>
        {step.s === "menu" && (
          <div class="file-menu">
            <button type="button" class="btn btn-neutral" onClick={onDone}>
              {t("cancel")}
            </button>
            {target.dir && (
              <Button needsLink onClick={() => setStep({ s: "new", dir: false })}>
                {t("files.new_file")}
              </Button>
            )}
            {target.dir && (
              <Button needsLink onClick={() => setStep({ s: "new", dir: true })}>
                {t("files.new_dir")}
              </Button>
            )}
            {target.dir && (
              <Button needsLink onClick={() => setStep({ s: "upload" })}>
                {t("xfer.upload")}
              </Button>
            )}
            {!target.dir && (
              <Button needsLink onClick={() => setStep({ s: "download" })}>
                {t("xfer.download")}
              </Button>
            )}
            {target.path !== "" && (
              <Button needsLink onClick={() => setStep({ s: "rename" })}>
                {t("files.rename")}
              </Button>
            )}
            {target.path !== "" && (
              <Button kind="danger" needsLink onClick={() => setStep({ s: "delete" })}>
                {t("files.delete")}
              </Button>
            )}
          </div>
        )}
        {step.s === "new" && (
          <NameForm
            label={step.dir ? t("files.new_dir") : t("files.new_file")}
            start=""
            verb={t("files.create")}
            onCancel={onDone}
            onSend={(name) =>
              run(step.dir ? "tree.new_dir" : "tree.new_file", { path: join(target.path, name) }, (p) => toast("ok", t("files.created", { path: p })))
            }
          />
        )}
        {step.s === "rename" && (
          <NameForm
            label={t("files.rename_to", { name: base(target.path) })}
            start={base(target.path)}
            verb={t("files.rename")}
            onCancel={onDone}
            onSend={(name) => run("tree.rename", { path: target.path, name }, () => toast("ok", t("files.renamed", { name })))}
          />
        )}
      </div>
    </Sheet>
  );
}
