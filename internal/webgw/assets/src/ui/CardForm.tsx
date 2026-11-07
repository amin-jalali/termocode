// An MCP server's question as a form on a card (MOB-12). Plain fields
// only (text, number, yes / no, a choice, long text): the laptop never
// puts a password or key field here. The answers go back with Send as
// strings; the laptop checks and converts them. Every control is at
// least 44 px tall (field-box), with its label above it.
import { h, type JSX } from "preact";
import type { BlockFormField } from "../../../../webproto/proto";
import { t } from "../strings";

export type FormValues = Record<string, string>;

/** formStart is the form's first values: the server's defaults. */
export function formStart(fields: readonly BlockFormField[]): FormValues {
  const out: FormValues = {};
  for (const f of fields) out[f.id] = f.value ?? "";
  return out;
}

/** formMissing is the label of the first required field left empty. */
export function formMissing(fields: readonly BlockFormField[], v: FormValues): string | undefined {
  return fields.find((f) => f.required === true && (v[f.id] ?? "").trim() === "")?.label;
}

export function CardForm({ id, fields, values, onChange }: { id: string; fields: readonly BlockFormField[]; values: FormValues; onChange: (v: FormValues) => void }) {
  const set = (k: string, val: string) => onChange({ ...values, [k]: val });
  const read = (e: Event) => (e.currentTarget as HTMLInputElement).value;
  return (
    <div class="card-form">
      {fields.map((f) => {
        const fid = `cf-${id}-${f.id}`;
        const v = values[f.id] ?? "";
        let input: JSX.Element;
        if (f.kind === "choice" || f.kind === "bool") {
          const opts = f.kind === "bool" ? [["true", t("card.form_yes")], ["false", t("card.form_no")]] : (f.choices ?? []).map((c) => [c, c]);
          input = (
            <select id={fid} class="field-box" value={v} required={f.required} onChange={(e: Event) => set(f.id, read(e))}>
              <option value="">{t("card.form_pick")}</option>
              {opts.map(([val, label]) => (
                <option key={val} value={val}>
                  {label}
                </option>
              ))}
            </select>
          );
        } else if (f.kind === "long") {
          input = <textarea id={fid} class="field-box" rows={3} maxLength={1000} value={v} required={f.required} onInput={(e: Event) => set(f.id, read(e))} />;
        } else {
          input = (
            <input
              id={fid}
              class="field-box"
              type="text"
              inputMode={f.kind === "num" ? "decimal" : undefined}
              autoComplete="off"
              maxLength={1000}
              value={v}
              required={f.required}
              onInput={(e: Event) => set(f.id, read(e))}
            />
          );
        }
        return (
          <label key={f.id} class="field" for={fid}>
            <span class="field-label">
              {f.label}
              {f.required && <span class="field-req"> · {t("card.form_required")}</span>}
            </span>
            {input}
            {f.hint && <span class="field-hint">{f.hint}</span>}
          </label>
        );
      })}
    </div>
  );
}
