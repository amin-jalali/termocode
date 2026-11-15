// The suggestion strip above the keyboard: a row of chips, gone (zero
// height) when there is nothing to suggest. Tapping a chip runs the
// accept callback and clears the strip.
import type { Suggestion } from "./completion";
import { setIcon } from "./icons";

export class SuggestionBar {
  readonly el: HTMLElement;
  items: Suggestion[] = [];
  /** Called when the strip appears or goes. */
  onChange?: () => void;
  private onAccept?: (s: Suggestion) => void;

  constructor(private doc: Document = document) {
    this.el = doc.createElement("div");
    this.el.className = "tk-sugg";
    this.el.hidden = true;
    this.el.setAttribute("role", "listbox");
    this.el.addEventListener("pointerdown", (e) => e.preventDefault()); // keep the editor's focus
    this.el.addEventListener("mousedown", (e) => e.preventDefault());
  }

  show(items: Suggestion[], onAccept: (s: Suggestion) => void): void {
    this.items = items;
    this.onAccept = onAccept;
    this.render();
  }

  clear(): void {
    this.items = [];
    this.onAccept = undefined;
    this.render();
  }

  accept(item: Suggestion): void {
    this.onAccept?.(item);
    this.clear();
  }

  private render(): void {
    const was = !this.el.hidden;
    const chips = this.items.map((it) => {
      const b = this.doc.createElement("button");
      b.className = "tk-chip";
      b.setAttribute("type", "button");
      b.setAttribute("role", "option");
      const ic = this.doc.createElement("span");
      ic.className = "tk-chip-icon";
      setIcon(ic, "code");
      const l = this.doc.createElement("span");
      l.className = "tk-chip-label";
      l.textContent = it.label;
      b.append(ic, l);
      if (it.detail) {
        const d = this.doc.createElement("span");
        d.className = "tk-chip-detail";
        d.textContent = it.detail;
        b.append(d);
      }
      b.addEventListener("click", () => this.accept(it));
      return b;
    });
    (this.el as unknown as { replaceChildren(...n: HTMLElement[]): void }).replaceChildren(...chips);
    this.el.hidden = this.items.length === 0;
    if (was !== !this.el.hidden) this.onChange?.();
  }
}
