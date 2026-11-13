// picker.js (WEB-55): the session picker's list, built alone beside app.js
// and loaded by it (lib/lazy.ts) once, after a passkey or bookmark
// sign-in. It imports types and pure helpers only; the page hands it
// Preact's h and its strings, so there is one Preact and the chunk adds no
// shared file to the page's first load. The page keeps the subscription
// and the choice (main.tsx PickerStep).
import type { h as PH } from "preact";
import type { HomeHost } from "../home/api";
import type { StringKey } from "../strings";
import { PS } from "./strings";
import { ago, pickerRows, type SessionRow } from "../lib/sessions";

export { pickerRows };
export type { SessionRow };

/** Host is what the page gives the list. */
export interface Host {
  h: typeof PH;
  t: (key: StringKey, vars?: Record<string, string | number>) => string;
  addStrings: (d: Readonly<Record<string, string>>) => void;
  onPick: (row?: SessionRow) => void;
}

/** Step is the whole step as a component: it follows the projects topic,
 * draws title, line, the rows once they are known and the way to stay on
 * this session, and picks this session at once when it is the only one. */
export function Step(x: Omit<Host, "h"> & { host: HomeHost }) {
  const { h, useEffect, link, store, go } = x.host;
  // A pick of another session opens its project page, then the step ends.
  const onPick = (row?: SessionRow) => {
    if (row) go({ tab: "more", rest: "projects/" + row.id, query: {} }, true);
    x.onPick();
  };
  const t = x.t;
  x.addStrings(PS);
  useEffect(() => link.subscribe("projects"), []);
  const topic = store.topic("projects").value;
  const rows = topic && (topic.data as { state?: string }).state === "ready" ? pickerRows(topic.data) : null;
  useEffect(() => {
    if (rows && rows.length <= 1) onPick();
  }, [rows?.length]);
  return h(
    "main",
    { class: "connect sp", id: "main", tabIndex: -1 },
    h("div", { class: "connect-top" }),
    h(
      "div",
      { class: "sp-box" },
      h("h1", { class: "sp-title" }, t("sp.title")),
      h("p", { class: "connect-line", role: rows ? undefined : "status" }, t(rows ? "sp.body" : "sp.loading")),
      rows && list({ ...x, h, onPick }, rows),
      h("button", { type: "button", class: "btn btn-neutral sp-skip", onClick: () => onPick() }, t("sp.continue")),
    ),
  );
}

/** list draws the rows: one 48 px button each (name, folder, branch,
 * waiting and working agents, last opened), then the way to stay here. */
function list(x: Host, rows: SessionRow[]) {
  const { h, t, onPick } = x;
  x.addStrings(PS);
  const when = (ms: number) => {
    const a = ago(ms, Date.now());
    return a.key === "" ? null : h("span", { class: "sp-when" }, t(a.key, { n: a.n }));
  };
  return h(
    "ul",
    { class: "sp-list", "aria-label": t("sp.title") },
    rows.map((r) =>
      h(
        "li",
        { key: r.id },
        h(
          "button",
          { type: "button", class: "sp-row" + (r.here ? " is-here" : ""), "data-key": r.id, onClick: () => onPick(r.here ? undefined : r) },
          h(
            "span",
            { class: "sp-main" },
            h("span", { class: "sp-name", dir: "auto" }, r.name, r.here && h("span", { class: "sp-tag" }, t("sp.here"))),
            r.where && h("span", { class: "sp-where", dir: "ltr" }, r.where),
            h(
              "span",
              { class: "sp-facts" },
              r.branch && h("span", { class: "sp-branch", dir: "ltr" }, "⎇ " + r.branch),
              r.waiting > 0 && h("span", { class: "sp-need" }, t("sp.waiting", { n: r.waiting })),
              r.working > 0 && h("span", null, t("sp.working", { n: r.working })),
              when(r.openedAt),
            ),
          ),
          h("span", { class: "sp-go", "aria-hidden": "true" }, "›"),
        ),
      ),
    ),
  );
}
