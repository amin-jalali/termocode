// The desktop's HTTP client (WEBP-HTTP), like the laptop's: the
// workspace's .http requests by file, the environment, the saved
// requests and the history in the side bar; a request typed here, a curl
// import and a new .http file as small forms on top; the answer in the
// panel's HTTP RESPONSE tab. The laptop sends every request with its own
// client and environments; tokens, cookies and private values come back
// hidden. The first request to a host that is not the laptop itself
// waits for a laptop card.
import { Fragment, h, type ComponentChildren } from "preact";
import { signal } from "@preact/signals";
import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import type { HTTPCurl, HTTPFiles, HTTPHistory, HTTPPath, HTTPReqItem, HTTPResponse, HTTPSaved, HTTPSent } from "../../../../../webproto/proto";
import { copyText } from "../../lib/clip";
import { panelTab } from "../../lib/lsp";
import { toast } from "../../lib/toasts";
import { httpForm } from "../../lib/views";
import { setPanes, showFile } from "../../lib/workbench";
import { link } from "../../session";
import { t, type StringKey } from "../../strings";
import { Cod } from "../../ui/icons";
import { Act, PaneSection, PaneView, Row, Tree } from "../../ui/pane";
import { gitFailed as failed } from "../git/errors";
import { ask, canAct } from "../run/bits";

const files = signal<HTTPFiles | null>(null);
const saved = signal<HTTPSaved | null>(null);
const hist = signal<HTTPHistory | null>(null);
/** resp is the answer the HTTP RESPONSE panel shows. */
const resp = signal<HTTPResponse | null>(null);
const note = signal("");
const shut = signal<ReadonlySet<string>>(new Set());

const loadFiles = () => ask("http.files", {}, (d) => (files.value = d as HTTPFiles));
const loadSaved = () => ask("http.saved", {}, (d) => (saved.value = d as HTTPSaved));
const loadHist = () => ask("http.history", {}, (d) => (hist.value = d as HTTPHistory));

function showPanel(): void {
  panelTab.value = "http-response";
  setPanes({ panel: true });
}

let watching = 0;

/** watch shows run in the panel, asking again while it runs. */
function watch(run: number): void {
  watching = run;
  showPanel();
  const poll = () =>
    link.request("http.response", { run }).then((d) => {
      if (watching !== run) return;
      const r = d as HTTPResponse;
      resp.value = r;
      if (r.running) setTimeout(poll, 300);
      else loadHist();
    }, failed);
  poll();
}

/** send asks the laptop to send one request. */
function send(args: Record<string, unknown>): void {
  note.value = "";
  link.request("http.send", args).then((d) => {
    const s = d as HTTPSent;
    if (s.card) note.value = t("http.asked", { host: s.host ?? "" });
    else if (s.run) watch(s.run);
  }, failed);
}

function showLast(): void {
  watching = 0;
  link.request("http.response", {}).then((d) => {
    resp.value = d as HTTPResponse;
    showPanel();
  }, failed);
}

const curl = (args: Record<string, unknown>) => ask("http.curl", args, (d) => copyText((d as HTTPCurl).curl));

function toggle(id: string): void {
  const n = new Set(shut.peek());
  if (!n.delete(id)) n.add(id);
  shut.value = n;
}

const METHODS = ["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "GRPC"];

/** Form is one small form: its fields and OK / Cancel (Cancel first). */
function Form({ label, ok, onOK, onCancel, children }: { label: string; ok: StringKey; onOK: () => void; onCancel: () => void; children: ComponentChildren }) {
  const box = useRef<HTMLFormElement | null>(null);
  useLayoutEffect(() => (box.current?.querySelector("input, textarea") as HTMLElement | null)?.focus(), []);
  return (
    <form
      ref={box}
      class="http-form"
      aria-label={label}
      onSubmit={(e) => (e.preventDefault(), onOK())}
      onKeyDown={(e) => e.key === "Escape" && (e.stopPropagation(), onCancel())}
    >
      {children}
      <div class="pv-ask-row">
        <button type="button" class="pv-btn is-2" onClick={onCancel}>
          {t("cancel")}
        </button>
        <button type="submit" class="pv-btn" data-act="ok">
          {t(ok)}
        </button>
      </div>
    </form>
  );
}

const val = (e: Event) => (e.currentTarget as HTMLInputElement).value;

/** One is a one-field form (a name, a path, a curl line). */
function One({ label, ok, init = "", area, onOK, onCancel }: { label: StringKey; ok: StringKey; init?: string; area?: boolean; onOK: (v: string) => void; onCancel: () => void }) {
  const [v, setV] = useState(init);
  return (
    <Form label={t(label)} ok={ok} onOK={() => v.trim() && onOK(v.trim())} onCancel={onCancel}>
      <div class="pv-input">
        {area ? (
          <textarea class="http-in" rows={3} aria-label={t(label)} placeholder={t(label)} value={v} onInput={(e) => setV(val(e))} />
        ) : (
          <input class="http-in" aria-label={t(label)} placeholder={t(label)} value={v} onInput={(e) => setV(val(e))} />
        )}
      </div>
    </Form>
  );
}

function Quick({ done }: { done: () => void }) {
  const [m, setM] = useState("GET");
  const [url, setUrl] = useState("");
  const [hdrs, setHdrs] = useState("");
  const [body, setBody] = useState("");
  const go = () => {
    if (!url.trim()) return;
    const args: Record<string, unknown> = { method: m, url: url.trim() };
    const hs = hdrs
      .split("\n")
      .map((l) => l.split(":"))
      .filter((p) => p.length > 1 && p[0].trim())
      .map((p) => ({ name: p[0].trim(), value: p.slice(1).join(":").trim() }));
    if (hs.length) args.headers = hs;
    if (body) args.body = body;
    send(args);
    done();
  };
  return (
    <Form label={t("http.new_req")} ok="send" onOK={go} onCancel={done}>
      <div class="http-line">
        <select class="http-method" aria-label={t("http.method")} value={m} onChange={(e) => setM(val(e))}>
          {METHODS.map((x) => (
            <option key={x} value={x}>
              {x}
            </option>
          ))}
        </select>
        <div class="pv-input">
          <input class="http-url" aria-label={t("http.url")} placeholder={t("http.url")} value={url} onInput={(e) => setUrl(val(e))} />
        </div>
      </div>
      <div class="pv-input">
        <textarea class="http-hdrs" rows={2} aria-label={t("http.headers")} placeholder={t("http.headers")} value={hdrs} onInput={(e) => setHdrs(val(e))} />
      </div>
      <div class="pv-input">
        <textarea class="http-body" rows={3} aria-label={t("http.body")} placeholder={t("http.body")} value={body} onInput={(e) => setBody(val(e))} />
      </div>
    </Form>
  );
}

function SideForm() {
  const f = httpForm.value;
  const done = () => (httpForm.value = "");
  if (f === "quick") return <Quick done={done} />;
  if (f === "import")
    return (
      <One
        label="http.import"
        ok="http.import_ok"
        area
        onCancel={done}
        onOK={(v) => {
          done();
          ask("http.import_curl", { curl: v }, (d) => {
            toast("ok", t("http.wrote", { path: (d as HTTPPath).path }));
            loadFiles();
          });
        }}
      />
    );
  if (f === "newfile")
    return (
      <One
        label="http.new_file"
        ok="files.create"
        onCancel={done}
        onOK={(v) => {
          done();
          const path = /\.(http|rest)$/i.test(v) ? v : v + ".http";
          ask("http.new_file", { path }, (d) => {
            toast("ok", t("http.wrote", { path: (d as HTTPPath).path }));
            loadFiles();
          });
        }}
      />
    );
  return null;
}

function reqLabel(r: HTTPReqItem): string {
  return r.name || `${r.method} ${r.url}`;
}

export function HTTPView() {
  useEffect(() => {
    loadFiles();
    loadSaved();
    loadHist();
  }, []);
  const can = canAct();
  const fs = files.value;
  const sv = saved.value?.requests ?? [];
  const hs = hist.value?.entries ?? [];
  const s = shut.value;
  return (
    <PaneView
      id="http"
      title={t("act.http")}
      actions={
        <>
          {can && <Act icon="add" act="http-quick" label={t("cmd.http_quick")} onClick={() => (httpForm.value = "quick")} />}
          {can && <Act icon="insert" act="http-import" label={t("http.import")} onClick={() => (httpForm.value = "import")} />}
          {can && <Act icon="new-file" act="http-new-file" label={t("http.new_file")} onClick={() => (httpForm.value = "newfile")} />}
          <Act icon="refresh" act="http-refresh" label={t("ctr.refresh")} onClick={() => (loadFiles(), loadSaved(), loadHist())} />
        </>
      }
    >
      <div class="pv-scroll">
        <div class="http-head">
          <label class="http-env-label">
            <Cod name="server-environment" />
            <select
              class="http-env"
              aria-label={t("dbv.f_env")}
              value={fs?.env ?? ""}
              disabled={!can || !fs}
              onChange={(e) => {
                const name = val(e);
                ask("http.env_select", { name }, () => fs && (files.value = { ...fs, env: name }));
              }}
            >
              <option value="">{t("http.env_none")}</option>
              {(fs?.envs ?? []).map((n) => (
                <option key={n} value={n}>
                  {n}
                </option>
              ))}
            </select>
            {can && <Act icon="go-to-file" act="http-env-file" label={t("http.env_file")} onClick={() => ask("http.env_file", {}, (d) => showFile((d as HTTPPath).path))} />}
            <Act icon="output" act="http-last" label={t("http.last")} onClick={showLast} />
          </label>
          {note.value && (
            <p class="http-note" role="status">
              {note.value}
            </p>
          )}
          <SideForm />
        </div>
        <PaneSection id="requests" title={t("http.requests")} open={!s.has("requests")} onToggle={() => toggle("requests")} count={fs?.files.reduce((n, f) => n + f.requests.length, 0)}>
          <Tree label={t("http.requests")}>
            {!fs ? (
              <Row label={t("loading")} />
            ) : fs.files.length === 0 ? (
              <Row label={t("http.none")} />
            ) : (
              fs.files.map((f) => (
                <Fragment key={f.path}>
                  <Row level={0} twist={s.has("f:" + f.path) ? "closed" : "open"} onToggle={() => toggle("f:" + f.path)} icon={<Cod name="file-code" />} label={f.path.split("/").pop() ?? f.path} desc={f.path.includes("/") ? f.path.slice(0, f.path.lastIndexOf("/")) : ""} data={{ file: f.path }} />
                  {!s.has("f:" + f.path) &&
                    f.requests.map((r) => (
                      <Row
                        key={r.index}
                        level={1}
                        icon={<span class="http-m">{r.method}</span>}
                        label={reqLabel(r)}
                        desc={r.name ? r.url : ""}
                        title={`${r.method} ${r.url}`}
                        data={{ req: `${f.path}#${r.index}` }}
                        onClick={() => showFile(f.path)}
                        actions={
                          <>
                            {can && <Act icon="play" act="send" label={t("send")} onClick={() => send({ path: f.path, index: r.index })} />}
                            <Act icon="copy" act="curl" label={t("http.curl")} onClick={() => curl({ path: f.path, index: r.index })} />
                            {can && <Act icon="save" act="save" label={t("http.save")} onClick={() => ask("http.save", { path: f.path, index: r.index, name: reqLabel(r) }, loadSaved)} />}
                          </>
                        }
                      />
                    ))}
                </Fragment>
              ))
            )}
          </Tree>
        </PaneSection>
        <PaneSection id="saved" title={t("http.saved")} open={!s.has("saved")} onToggle={() => toggle("saved")} count={sv.length} fixed>
          <Tree label={t("http.saved")}>
            {sv.map((r) => (
              <Row key={r.index} level={0} icon={<span class="http-m">{r.method}</span>} label={reqLabel(r)} desc={r.name ? r.url : ""} data={{ saved: String(r.index) }} actions={can && <Act icon="play" act="send" label={t("send")} onClick={() => send({ saved: r.index })} />} />
            ))}
          </Tree>
        </PaneSection>
        <PaneSection id="history" title={t("http.history")} open={!s.has("history")} onToggle={() => toggle("history")} count={hs.length} fixed>
          <Tree label={t("http.history")}>
            {hs.map((e) => (
              <Row
                key={e.id}
                level={0}
                icon={<span class={`http-m ${e.err || (e.status ?? 0) >= 400 ? "c-err" : "c-ok"}`}>{e.method}</span>}
                label={e.name || e.url}
                desc={e.err ? e.err : `${e.status ?? ""} · ${e.elapsed_ms ?? 0} ms`}
                title={`${e.method} ${e.url}`}
                data={{ hist: e.id }}
                actions={can && <Act icon="play" act="send" label={t("edit.send_again")} onClick={() => send({ history: e.id })} />}
              />
            ))}
          </Tree>
        </PaneSection>
      </div>
    </PaneView>
  );
}

/** pretty indents a JSON body; any other text stays as it came. */
function pretty(r: HTTPResponse): string {
  const b = r.body ?? "";
  if (!/json/.test(r.content_type ?? "") || b.length > 1 << 20) return b;
  try {
    return JSON.stringify(JSON.parse(b), null, 2);
  } catch {
    return b;
  }
}

function Heads({ title, list }: { title: string; list: { name: string; value: string }[] }) {
  return (
    <>
      <h3 class="http-h">{title}</h3>
      <dl class="http-hdr">
        {list.map((x, i) => (
          <Fragment key={i}>
            <dt>{x.name}</dt>
            <dd>{x.value}</dd>
          </Fragment>
        ))}
      </dl>
    </>
  );
}

/** HTTPPanel is the panel's HTTP RESPONSE tab. */
export function HTTPPanel() {
  const r = resp.value;
  const [tab, setTab] = useState<"body" | "headers">("body");
  const [form, setForm] = useState<"" | "save" | "body">("");
  if (!r) return <p class="http-empty">{t("http.no_response")}</p>;
  const can = canAct();
  const ok = !r.err && (r.status ?? 0) > 0 && (r.status ?? 0) < 400;
  return (
    <section class="http-resp" aria-label={t("http.response")}>
      <header class="http-bar">
        <span class={r.running ? "" : ok ? "c-ok" : "c-err"}>
          <Cod name={r.running ? "loading" : ok ? "pass-filled" : "error"} />
        </span>
        <span class="http-status" dir={r.running ? "auto" : "ltr"}>{r.running ? t("live.sending") : r.err || `${r.status ?? ""} ${r.status_text ?? ""}`}</span>
        <span class="http-what" dir="ltr" title={`${r.method} ${r.url}`}>
          {r.method} {r.url}
        </span>
        {!r.running && <span class="tr-age" dir="ltr">{`${r.elapsed_ms ?? 0} ms · ${r.size ?? 0} B`}</span>}
        <span class="http-acts">
          {r.running && can && <Act icon="stop-circle" act="cancel" label={t("http.cancel")} onClick={() => ask("http.cancel")} />}
          <Act icon="copy" act="curl" label={t("http.curl")} onClick={() => curl({ run: r.run })} />
          {can && !r.running && <Act icon="save" act="save" label={t("http.save")} onClick={() => setForm("save")} />}
          {can && !r.running && r.body && <Act icon="new-file" act="save-body" label={t("http.save_body")} onClick={() => setForm("body")} />}
        </span>
      </header>
      {form === "save" && (
        <One label="http.name" ok="file.save" init={r.label} onCancel={() => setForm("")} onOK={(name) => (setForm(""), ask("http.save", { run: r.run, name }, () => (toast("ok", t("http.saved_as", { name })), loadSaved())))} />
      )}
      {form === "body" && (
        <One label="http.file" ok="file.save" onCancel={() => setForm("")} onOK={(path) => (setForm(""), ask("http.save_body", { run: r.run, path }, (d) => toast("ok", t("http.wrote", { path: (d as HTTPPath).path }))))} />
      )}
      <div class="http-tabs" role="tablist">
        {(["body", "headers"] as const).map((x) => (
          <button key={x} type="button" role="tab" class={`http-tab${tab === x ? " is-on" : ""}`} data-tab={x} aria-selected={tab === x ? "true" : "false"} onClick={() => setTab(x)}>
            {t(x === "body" ? "http.body" : "http.headers_tab")}
          </button>
        ))}
      </div>
      {tab === "body" ? (
        r.binary ? (
          <p class="http-empty">{t("http.binary")}</p>
        ) : (
          <pre class="http-body-text" dir="ltr">
            {pretty(r)}
          </pre>
        )
      ) : (
        <div class="http-body-text">
          <Heads title={t("http.response")} list={r.headers} />
          <Heads title={t("http.request")} list={r.req_headers} />
          {(r.hops ?? []).length > 0 && <Heads title={t("http.hops")} list={(r.hops ?? []).map((h) => ({ name: "→", value: h }))} />}
        </div>
      )}
    </section>
  );
}
