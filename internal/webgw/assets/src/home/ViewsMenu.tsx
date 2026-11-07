// The Views menu (MOB-9, mobile plan 2.2): Panels → All views. The
// laptop's registry (view.catalog) by feature in its order, a search box,
// and for a view with params a small form built from them; Show puts the
// view's block in the Home stream. Asking costs no tokens.
import { h, H } from "./jsx";
import { hs, type HomeKey } from "./strings";
import { dict, has } from "../state/dict";
import { byFeature, readCatalog, type ViewCatalog, type ViewDef, type ViewParam, type ViewSpec } from "./views";

/** The catalog, read once per page (the registry changes only with a
 * new laptop build). */
let cached: ViewCatalog | null = null;
let asking: Promise<ViewCatalog> | null = null;

/** catalog is the laptop's view registry for this connection. */
export function catalog(): Promise<ViewCatalog> {
  if (cached) return Promise.resolve(cached);
  if (!asking) {
    asking = H.link.request("view.catalog", {}).then(
      (d) => {
        const c = readCatalog(d);
        if (!c) throw new Error("bad catalog");
        cached = c;
        return c;
      },
      (e: unknown) => {
        asking = null;
        throw e;
      },
    );
  }
  return asking;
}

const FEATURE: Readonly<Record<string, HomeKey>> = {
  git: "feature.git", lanes: "feature.lanes", runs: "feature.runs", workflows: "feature.workflows", tasks: "feature.tasks",
  projects: "feature.projects", inbox: "feature.inbox", chat: "feature.chat", ci: "feature.ci", mcp: "feature.mcp",
};

function featureLabel(f: string): string {
  return has(FEATURE, f) ? hs(FEATURE[f]) : f;
}

function ParamField({ p, value, onInput }: { p: ViewParam; value: string; onInput: (v: string) => void }) {
  const id = "vp-" + p.id;
  if (p.kind === "enum" && p.enum?.length) {
    return (
      <label class="home-field" for={id}>
        <span>{p.label}</span>
        <select id={id} value={value} onChange={(e) => onInput((e.currentTarget as HTMLSelectElement).value)}>
          {!p.required && <option value="">{hs("views.choose")}</option>}
          {p.enum.map((v) => (
            <option key={v} value={v}>
              {v}
            </option>
          ))}
        </select>
      </label>
    );
  }
  const numeric = p.kind === "int" || p.kind === "number_pr";
  return (
    <label class="home-field" for={id}>
      <span>{p.label}</span>
      <input
        id={id}
        type={numeric ? "number" : "text"}
        inputMode={numeric ? "numeric" : undefined}
        min={numeric ? 1 : undefined}
        autoComplete="off"
        autocapitalize="off"
        spellcheck={false}
        value={value}
        required={p.required}
        onInput={(e) => onInput((e.currentTarget as HTMLInputElement).value)}
      />
    </label>
  );
}

function Params({ def, onRun, onBack }: { def: ViewDef; onRun: (s: ViewSpec) => void; onBack: () => void }) {
  const [args, setArgs] = H.useState<Record<string, string>>(() => ({ ...(def.default.args ?? {}) }));
  const [why, setWhy] = H.useState("");
  const submit = (e: Event) => {
    e.preventDefault();
    const missing = (def.params ?? []).find((p) => p.required && !(args[p.id] ?? "").trim());
    if (missing) return setWhy(hs("views.need", { label: missing.label }));
    const clean = dict<string>();
    for (const p of def.params ?? []) if ((args[p.id] ?? "").trim()) clean[p.id] = args[p.id].trim();
    onRun(Object.keys(clean).length ? { view: def.id, args: clean } : { view: def.id });
  };
  return (
    <form class="home-panels home-form" onSubmit={submit}>
      <h2 class="home-panels-title" dir="auto">
        {def.title}
      </h2>
      {(def.params ?? []).map((p) => (
        <ParamField key={p.id} p={p} value={args[p.id] ?? ""} onInput={(v) => setArgs({ ...args, [p.id]: v })} />
      ))}
      {why && (
        <p class="home-status" role="alert">
          {why}
        </p>
      )}
      <div class="home-actions">
        <button type="button" class="btn" onClick={onBack}>
          {hs("views.back")}
        </button>
        <button type="submit" class="btn btn-primary">
          {hs("views.show")}
        </button>
      </div>
    </form>
  );
}

export function ViewsMenu({ onRun, onClose }: { onRun: (s: ViewSpec) => void; onClose: () => void }) {
  const S = H.Sheet;
  const [cat, setCat] = H.useState<ViewCatalog | null>(cached);
  const [err, setErr] = H.useState("");
  const [q, setQ] = H.useState("");
  const [pick, setPick] = H.useState<ViewDef | null>(null);
  H.useEffect(() => {
    if (cached) return;
    catalog().then(setCat, (e: unknown) => setErr(H.errText(e)));
  }, []);
  const run = (s: ViewSpec) => {
    onClose();
    onRun(s);
  };
  const open = (d: ViewDef) => (d.params?.length ? setPick(d) : run({ view: d.id }));
  if (pick) {
    return (
      <S onLater={onClose} label={pick.title}>
        <Params def={pick} onRun={run} onBack={() => setPick(null)} />
      </S>
    );
  }
  const groups = cat ? byFeature(cat, q) : [];
  return (
    <S onLater={onClose} label={hs("views.title")}>
      <div class="home-panels">
        <h2 class="home-panels-title">{hs("views.title")}</h2>
        <label class="home-field">
          <span class="sr-only">{hs("views.search")}</span>
          <input type="search" placeholder={hs("views.search")} value={q} autoComplete="off" onInput={(e) => setQ((e.currentTarget as HTMLInputElement).value)} />
        </label>
        {!cat && !err && <p class="home-status">{hs("views.loading")}</p>}
        {err && (
          <p class="home-status" role="alert">
            {err}
          </p>
        )}
        {cat && groups.length === 0 && <p class="home-status">{hs("views.none")}</p>}
        {groups.map((g) => (
          <section key={g.feature} aria-labelledby={"vf-" + g.feature}>
            <h3 class="home-group-title" id={"vf-" + g.feature}>
              {featureLabel(g.feature)}
            </h3>
            <ul class="home-rows">
              {g.views.map((d) => (
                <li key={d.id}>
                  <button type="button" class="home-row" onClick={() => open(d)}>
                    <span class="home-row-main">
                      <span class="home-row-title trunc" dir="auto">
                        {d.title}
                      </span>
                      <span class="home-row-sub mono trunc">{d.id}</span>
                    </span>
                    {d.live && <span class="home-row-badge">{hs("views.live")}</span>}
                  </button>
                </li>
              ))}
            </ul>
          </section>
        ))}
        <button type="button" class="btn home-panels-close" onClick={onClose}>
          {hs("panels.close")}
        </button>
      </div>
    </S>
  );
}
