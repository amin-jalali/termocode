// A row's menu (WEB-36): what a long press or a right click offers. It
// is a sheet, so Esc, the dim area and a drag down close it and focus
// goes back where it was.
import { h } from "preact";
import { t } from "../strings";
import { Sheet } from "./Sheet";

export interface MenuItem {
  label: string;
  pick: () => void;
}

export function RowMenu({ title, items, onClose }: { title: string; items: MenuItem[]; onClose: () => void }) {
  return (
    <Sheet onLater={onClose} label={title}>
      <section class="row-menu" role="dialog" aria-modal="true" aria-labelledby="row-menu-t">
        <h2 class="row-menu-title trunc" id="row-menu-t" dir="auto">
          {title}
        </h2>
        <ul class="row-menu-list">
          {items.map((it) => (
            <li key={it.label}>
              <button
                type="button"
                class="row-menu-item"
                onClick={() => {
                  onClose();
                  it.pick();
                }}
              >
                {it.label}
              </button>
            </li>
          ))}
        </ul>
        <button type="button" class="row-menu-item row-menu-cancel" onClick={onClose}>
          {t("cancel")}
        </button>
      </section>
    </Sheet>
  );
}
