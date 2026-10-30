// The terminal's input (WEB-20, web UX 6.3): a command line with Send and
// the key bar: Esc Tab Ctrl Alt ← ↓ ↑ → ^C Paste. Ctrl and Alt are sticky
// (one tap = the next key, a second tap locks, a third lets go). ^C is
// term.interrupt. Paste opens a sheet: the browser's own paste works in
// its text box, so the page never reads the clipboard. Offline every key
// locks and nothing is kept to send later (web UX P5).
import { h } from "preact";
import { signal } from "@preact/signals";
import { useState } from "preact/hooks";
import { toast } from "../lib/toasts";
import { OfflineError } from "../net/link";
import { link, live } from "../session";
import { t, type StringKey } from "../strings";
import { Button } from "../ui/Button";
import { errText } from "../ui/CardSheet";
import { Sheet } from "../ui/Sheet";
import { barKeys, type KeyId } from "../lib/keys";
import { prefs } from "../lib/prefs";
import { KeysSheet } from "./KeysSheet";

/** A modifier: 0 off, 1 for the next key, 2 locked. */
type Mod = 0 | 1 | 2;
export const ctrl = signal<Mod>(0);
export const alt = signal<Mod>(0);
/** pasting is true while the paste sheet is open (the key or a row's menu opens it). */
export const pasting = signal(false);

/** MAX_PASTE is the most one paste sends (term.send takes 64 KiB). */
export const MAX_PASTE = 64 << 10;
/** ASK_FROM: a paste this big asks first. */
const ASK_FROM = 1024;

function bytes(s: string): number {
  return new TextEncoder().encode(s).length;
}

function nextMod(m: Mod): Mod {
  return m === 0 ? 1 : m === 1 ? 2 : 0;
}

/** used lets go of the modifiers that were for one key only. */
function used(): void {
  if (ctrl.peek() === 1) ctrl.value = 0;
  if (alt.peek() === 1) alt.value = 0;
}

/** withMods turns one typed character into what the shell gets. */
export function withMods(ch: string, c: boolean, a: boolean): string {
  let out = ch;
  if (c && ch.length === 1) {
    const code = ch.toUpperCase().charCodeAt(0);
    if (code >= 64 && code <= 95) out = String.fromCharCode(code & 0x1f);
    else if (ch === " ") out = "\x00";
  }
  return a ? "\x1b" + out : out;
}

/** arrow is an arrow key's bytes, with Ctrl or Alt as xterm sends them. */
function arrow(dir: string, c: boolean, a: boolean): string {
  const m = c && a ? 7 : c ? 5 : a ? 3 : 0;
  return m ? `\x1b[1;${m}${dir}` : `\x1b[${dir}`;
}

function sendText(buf: number, text: string): Promise<boolean> {
  if (!live.peek() || !text) return Promise.resolve(false);
  return link.request("term.send", { buf, text }).then(
    () => true,
    (e: unknown) => {
      if (!(e instanceof OfflineError)) toast("warn", errText(e));
      return false;
    },
  );
}

export interface Key {
  label: StringKey;
  aria?: StringKey;
  cls?: string;
  press: (buf: number) => void;
  mod?: typeof ctrl;
}

/** KEYS: what each key of lib/keys.ts shows and sends. */
export const KEYS: Record<KeyId, Key> = {
  esc: { label: "key.esc", press: (b) => send(b, "\x1b") },
  tab: { label: "key.tab", press: (b) => send(b, "\t") },
  ctrl: { label: "key.ctrl", aria: "key.ctrl.label", press: () => (ctrl.value = nextMod(ctrl.peek())), mod: ctrl },
  alt: { label: "key.alt", aria: "key.alt.label", press: () => (alt.value = nextMod(alt.peek())), mod: alt },
  left: { label: "key.left", press: (b) => sendArrow(b, "D") },
  down: { label: "key.down", press: (b) => sendArrow(b, "B") },
  up: { label: "key.up", press: (b) => sendArrow(b, "A") },
  right: { label: "key.right", press: (b) => sendArrow(b, "C") },
  int: {
    label: "key.interrupt",
    aria: "key.interrupt.label",
    cls: "key-int",
    press: (b) => {
      link.request("term.interrupt", { buf: b }).catch((e: unknown) => {
        if (!(e instanceof OfflineError)) toast("warn", errText(e));
      });
    },
  },
  home: { label: "key.home", press: (b) => sendArrow(b, "H") },
  end: { label: "key.end", press: (b) => sendArrow(b, "F") },
  pgup: { label: "key.pgup", press: (b) => send(b, "\x1b[5~") },
  pgdn: { label: "key.pgdn", press: (b) => send(b, "\x1b[6~") },
  del: { label: "key.del", press: (b) => send(b, "\x1b[3~") },
  ctrl_a: { label: "key.ctrl_a", aria: "key.ctrl_a.label", press: (b) => send(b, "\x01") },
  ctrl_d: { label: "key.ctrl_d", aria: "key.ctrl_d.label", press: (b) => send(b, "\x04") },
  ctrl_e: { label: "key.ctrl_e", aria: "key.ctrl_e.label", press: (b) => send(b, "\x05") },
  ctrl_l: { label: "key.ctrl_l", aria: "key.ctrl_l.label", press: (b) => send(b, "\x0c") },
  ctrl_r: { label: "key.ctrl_r", aria: "key.ctrl_r.label", press: (b) => send(b, "\x12") },
  ctrl_z: { label: "key.ctrl_z", aria: "key.ctrl_z.label", press: (b) => send(b, "\x1a") },
  pipe: { label: "key.pipe", press: (b) => send(b, "|") },
  tilde: { label: "key.tilde", press: (b) => send(b, "~") },
  slash: { label: "key.slash", press: (b) => send(b, "/") },
  dash: { label: "key.dash", press: (b) => send(b, "-") },
};

function send(buf: number, raw: string): void {
  const text = alt.peek() ? "\x1b" + raw : raw;
  used();
  sendText(buf, text);
}

function sendArrow(buf: number, dir: string): void {
  const text = arrow(dir, ctrl.peek() > 0, alt.peek() > 0);
  used();
  sendText(buf, text);
}

function PasteSheet({ buf, onClose }: { buf: number; onClose: () => void }) {
  const [text, setText] = useState("");
  const [ask, setAsk] = useState(false);
  const [note, setNote] = useState<string | null>(null);
  const lines = text.split("\n").length;
  const go = () => {
    if (!text || !live.peek()) return;
    if (bytes(text) > MAX_PASTE) {
      setNote(t("err.limit"));
      return;
    }
    if (bytes(text) >= ASK_FROM && !ask) {
      setAsk(true);
      return;
    }
    sendText(buf, text).then((ok) => ok && onClose());
  };
  return (
    <Sheet onLater={onClose} label={t("term.paste.title")}>
      <section class="edit paste" role="dialog" aria-modal="true" aria-labelledby="paste-t">
        <h2 class="edit-title" id="paste-t">
          {t("term.paste.title")}
        </h2>
        {note && (
          <p class="composer-note" role="alert">
            {note}
          </p>
        )}
        <textarea
          class="edit-box"
          dir="ltr"
          spellcheck={false}
          autocapitalize="off"
          autocomplete="off"
          value={text}
          aria-labelledby="paste-t"
          onInput={(e) => {
            setText((e.currentTarget as HTMLTextAreaElement).value);
            setAsk(false);
            setNote(null);
          }}
        />
        {ask && (
          <p class="file-note paste-ask" role="alert">
            {t("term.paste.many", { n: lines })}
          </p>
        )}
        <div class="edit-actions">
          <Button kind="neutral" onClick={onClose}>
            {t("cancel")}
          </Button>
          <span class="paste-send">
            <Button kind="primary" needsLink disabled={!text} onClick={go}>
              {t("send")}
            </Button>
          </span>
        </div>
      </section>
    </Sheet>
  );
}

export function TermInput({ buf, ended }: { buf: number; ended: boolean }) {
  const [line, setLine] = useState("");
  const [editing, setEditing] = useState(false);
  const off = !live.value || ended;
  const submit = () => {
    if (off) return;
    const text = line;
    sendText(buf, text + "\r").then((ok) => ok && setLine((cur) => (cur === text ? "" : cur)));
  };
  return (
    <div class={`composer term-input${off ? " is-off" : ""}`}>
      <div class="composer-row">
        <input
          class="composer-box term-line"
          type="text"
          dir="ltr"
          value={line}
          disabled={ended}
          autocomplete="off"
          autocapitalize="off"
          spellcheck={false}
          enterkeyhint="send"
          placeholder={t("term.input")}
          aria-label={t("term.input")}
          onBeforeInput={(e: InputEvent) => {
            // A sticky modifier turns the next typed character into a key.
            const ch = e.data;
            if (!ch || ch.length !== 1 || (ctrl.peek() === 0 && alt.peek() === 0)) return;
            e.preventDefault();
            if (off) return;
            const text = withMods(ch, ctrl.peek() > 0, alt.peek() > 0);
            used();
            sendText(buf, text);
          }}
          onInput={(e) => setLine((e.currentTarget as HTMLInputElement).value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.isComposing) {
              e.preventDefault();
              submit();
            }
          }}
        />
        <span class="send">
          <Button kind="primary" needsLink disabled={ended || !line} onClick={submit}>
            {t("send")}
          </Button>
        </span>
      </div>
      <div class="keybar" role="toolbar" aria-label={t("tab.term")}>
        {!live.value && <span class="chip keybar-off">{t("offline.input")}</span>}
        {barKeys(prefs.value.keys).map((id) => {
          const k = KEYS[id];
          const m = k.mod?.value ?? 0;
          return (
            <button
              key={id}
              type="button"
              class={`key${k.cls ? " " + k.cls : ""}${m === 1 ? " is-on" : ""}${m === 2 ? " is-locked" : ""}`}
              aria-label={k.aria ? t(k.aria) : undefined}
              aria-pressed={k.mod ? (m > 0 ? "true" : "false") : undefined}
              aria-disabled={off ? "true" : undefined}
              onClick={() => {
                if (!off) k.press(buf);
              }}
            >
              {t(k.label)}
            </button>
          );
        })}
        <button type="button" class="key key-edit" aria-label={t("key.edit.label")} onClick={() => setEditing(true)}>
          {t("key.edit")}
        </button>
        <button type="button" class="key key-paste" aria-disabled={off ? "true" : undefined} onClick={() => !off && (pasting.value = true)}>
          {t("key.paste")}
        </button>
      </div>
      {pasting.value && !off && <PasteSheet buf={buf} onClose={() => (pasting.value = false)} />}
      {editing && <KeysSheet keys={KEYS} onClose={() => setEditing(false)} />}
    </div>
  );
}
