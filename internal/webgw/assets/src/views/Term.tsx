// The terminal's shared parts (WEB-20, web UX 3.3, 5.6): a full-screen
// program's grid (Screen), the ⓘ sheet (About) and Open a new one. The
// phone's view is views/phone/PhoneTerm.tsx (home.js), the desktop's
// DeskTerm.tsx.
import { h } from "preact";
import { signal } from "@preact/signals";
import type { TermNewRes } from "../../../../webproto/proto";
import { prefs, setPref } from "../lib/prefs";
import { canKeepOn } from "../lib/wakelock";
import { go } from "../lib/router";
import { colorsOn, dropColors } from "../lib/termcolors";
import { toast } from "../lib/toasts";
import { ReqError } from "../net/link";
import { link } from "../session";
import { t } from "../strings";
import { Button } from "../ui/Button";
import { errText } from "../ui/CardSheet";
import { Chip } from "../ui/Chip";
import { FontSize } from "../ui/FontSize";
import { Sheet } from "../ui/Sheet";


/** wrapRows: long rows wrap (on) or scroll sideways (off). */
export const wrapRows = signal(true);
/** asRows holds the shells shown as rows although screen mode is on
 * (the guess can be wrong; the Rows toggle fixes it). */
export const asRows = signal<ReadonlySet<number>>(new Set());

function setAsRows(buf: number, on: boolean): void {
  const next = new Set(asRows.peek());
  if (on) next.add(buf);
  else next.delete(buf);
  asRows.value = next;
}

/** showOnLaptop shows the shell's tab on the laptop. */
export function showOnLaptop(buf: number): void {
  link.request("term.show_on_laptop", { buf }).then(
    () => toast("ok", t("laptop.shown")),
    (e: unknown) => {
      if (e instanceof ReqError) toast("warn", errText(e));
    },
  );
}

/** Screen is a full-screen program's screen at the laptop's size: a fixed
 * grid, read-only, that scrolls sideways; keys still work. */
export function Screen({ buf, rows, cols, height }: { buf: number; rows: readonly string[]; cols: number; height: number }) {
  const shown = height > 0 ? rows.slice(-height) : rows;
  return (
    <div class="term-screen-box">
      <div class="file-note is-info term-banner" role="status">
        <span>{t("term.screen_banner", { cols, rows: height || shown.length })}</span>
        <Button kind="neutral" small needsLink onClick={() => showOnLaptop(buf)}>
          {t("laptop.open")}
        </Button>
        <Button kind="quiet" small onClick={() => setAsRows(buf, true)}>
          {t("term.rows")}
        </Button>
      </div>
      <pre class="term-screen" dir="ltr" style={cols > 0 ? { width: cols + "ch" } : undefined} aria-readonly="true">
        {shown.join("\n")}
      </pre>
    </div>
  );
}

/** About is the ⓘ sheet: the honest width note and the view's toggles. */
export function About({ buf, cols, screen, onClose, wrap = true }: { buf: number; cols: number; screen: boolean; onClose: () => void; wrap?: boolean }) {
  return (
    <Sheet onLater={onClose} label={t("term.about")}>
      <section class="edit term-about" role="dialog" aria-modal="true" aria-labelledby="term-about-t">
        <h2 class="edit-title" id="term-about-t">
          {t("term.about")}
        </h2>
        <p class="group-text">{t("term.info", { cols: cols || 80 })}</p>
        <div class="term-toggles">
          {wrap && (
            <Chip on={wrapRows.value} onClick={() => (wrapRows.value = !wrapRows.peek())}>
              {t("term.wrap")}
            </Chip>
          )}
          <Chip on={screen && !asRows.value.has(buf)} onClick={() => setAsRows(buf, !asRows.peek().has(buf))}>
            {t("term.screen")}
          </Chip>
          {canKeepOn() && (
            <Chip on={prefs.value.wakeLock} onClick={() => setPref("wakeLock", !prefs.peek().wakeLock)}>
              {t("term.keep_on")}
            </Chip>
          )}
          <Chip
            on={colorsOn.value}
            onClick={() => {
              colorsOn.value = !colorsOn.peek();
              if (!colorsOn.peek()) dropColors();
            }}
          >
            {t("term.colors")}
          </Chip>
        </div>
        <p class="group-text term-colors-note">{t("term.colors_note")}</p>
        <FontSize />
        <div class="edit-actions">
          <Button kind="neutral" onClick={onClose}>
            {t("close")}
          </Button>
          <Button kind="primary" needsLink onClick={() => showOnLaptop(buf)}>
            {t("laptop.open")}
          </Button>
        </div>
      </section>
    </Sheet>
  );
}

/** newTerm starts a shell on the laptop and shows it here. */
export function newTerm(): void {
  link.request("term.new").then(
    (data) => go({ tab: "term", rest: String((data as TermNewRes).buf), query: {} }),
    (e: unknown) => {
      if (e instanceof ReqError) toast("warn", errText(e));
    },
  );
}
