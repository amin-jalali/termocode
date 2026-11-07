// Composer: the box at the bottom, in the thumb zone. Attached items show
// as chips (a reference only: id or path, never secret contents) with an
// × each; Enter adds a line on a phone, Send sends. Offline, Send is
// greyed and does nothing (inputs change state, not shape).
import { h } from "preact";
import { t } from "../strings";

export interface Attached {
  id: string;
  /** label is the short name shown on the chip (data). */
  label: string;
}

export function Composer({
  value,
  onInput,
  onSend,
  attached,
  onRemove,
  offline,
  placeholder,
}: {
  value: string;
  onInput: (v: string) => void;
  onSend: () => void;
  attached?: readonly Attached[];
  onRemove?: (id: string) => void;
  offline?: boolean;
  placeholder?: string;
}) {
  const empty = value.trim() === "" && (!attached || attached.length === 0);
  const off = offline || empty;
  return (
    <form
      class="ph-composer"
      onSubmit={(e) => {
        e.preventDefault();
        if (!off) onSend();
      }}
    >
      {attached && attached.length > 0 && (
        <div class="ph-composer-attached">
          {attached.map((a) => (
            <span key={a.id} class="ph-attach">
              <span>{a.label}</span>
              {onRemove && (
                <button type="button" class="ph-attach-x" aria-label={t("keys.remove_name", { name: a.label })} onClick={() => onRemove(a.id)}>
                  ×
                </button>
              )}
            </span>
          ))}
        </div>
      )}
      <div class="ph-composer-row">
        <textarea
          class="ph-composer-box"
          rows={1}
          value={value}
          placeholder={placeholder ?? t("ph.ask")}
          aria-label={placeholder ?? t("ph.ask")}
          enterkeyhint="send"
          onInput={(e) => onInput((e.currentTarget as HTMLTextAreaElement).value)}
        />
        <button type="submit" class="ph-composer-send" aria-disabled={off ? "true" : undefined}>
          {t("send")}
        </button>
      </div>
    </form>
  );
}
