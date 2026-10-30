// The key bar editor (WEB-56): the keys in the bar, in order, each with
// up, down and Remove (buttons, no dragging: WCAG 2.5.7), then the keys
// that can be added, then "Default keys". Paste and this editor's own
// key always stay at the bar's end. Changes show at once and are kept
// on this device as key names.
import { h } from "preact";
import { barKeys, DEFAULT_KEYS, KEY_IDS, move, type KeyId } from "../lib/keys";
import { prefs, setPref } from "../lib/prefs";
import { t, type StringKey } from "../strings";
import { Button } from "../ui/Button";
import { Sheet } from "../ui/Sheet";

interface Label {
  label: StringKey;
  aria?: StringKey;
}

function same(a: readonly KeyId[], b: readonly KeyId[]): boolean {
  return a.length === b.length && a.every((x, i) => x === b[i]);
}

/** save stores the bar; the default bar is stored as none. */
function save(keys: readonly KeyId[]): void {
  setPref("keys", same(keys, DEFAULT_KEYS) ? [] : [...keys]);
}

export function KeysSheet({ keys, onClose }: { keys: Record<KeyId, Label>; onClose: () => void }) {
  const bar = barKeys(prefs.value.keys);
  const rest = KEY_IDS.filter((id) => !bar.includes(id));
  const name = (id: KeyId) => t(keys[id].label);
  const full = (id: KeyId) => {
    const a = keys[id].aria;
    return a ? t(a) : name(id);
  };
  return (
    <Sheet onLater={onClose} label={t("keys.title")}>
      <section class="edit keys-edit" role="dialog" aria-modal="true" aria-labelledby="keys-t">
        <h2 class="edit-title" id="keys-t">
          {t("keys.title")}
        </h2>
        <p class="group-text">{t("keys.body")}</p>
        <ol class="keys-list" aria-label={t("keys.in_bar")}>
          {bar.map((id, i) => (
            <li key={id} class="keys-row">
              <span class="keys-name">{name(id)}</span>
              <button type="button" class="key keys-up" aria-label={t("keys.up", { name: full(id) })} disabled={i === 0} onClick={() => save(move(bar, i, -1))}>
                {t("key.up")}
              </button>
              <button
                type="button"
                class="key keys-down"
                aria-label={t("keys.down", { name: full(id) })}
                disabled={i === bar.length - 1}
                onClick={() => save(move(bar, i, 1))}
              >
                {t("key.down")}
              </button>
              <button
                type="button"
                class="key keys-remove"
                aria-label={t("keys.remove_name", { name: full(id) })}
                disabled={bar.length === 1}
                onClick={() => save(bar.filter((x) => x !== id))}
              >
                {t("keys.remove")}
              </button>
            </li>
          ))}
        </ol>
        {rest.length > 0 && (
          <div class="keys-more" role="group" aria-labelledby="keys-add-t">
            <h3 class="field-label" id="keys-add-t">
              {t("keys.add")}
            </h3>
            <div class="keys-adds">
              {rest.map((id) => (
                <button key={id} type="button" class="key keys-add" aria-label={t("keys.add_name", { name: full(id) })} onClick={() => save([...bar, id])}>
                  {name(id)}
                </button>
              ))}
            </div>
          </div>
        )}
        <div class="edit-actions">
          <Button kind="neutral" disabled={prefs.value.keys.length === 0} onClick={() => setPref("keys", [])}>
            {t("keys.default")}
          </Button>
          <Button kind="primary" onClick={onClose}>
            {t("done")}
          </Button>
        </div>
      </section>
    </Sheet>
  );
}
