// The chat's More Actions (web parity, AI chat): what the laptop's AI
// palette does for a chat, from the web. Rename, fork and export act at
// once; delete and clear cannot be undone, so they ask here first (Cancel
// first) and then wait for the laptop's card. The laptop's own AI
// commands (palette.list: its allow-list) run there by id; one that
// changes a setting, spends tokens or pushes asks the same way. The
// laptop's AI screens (usage, rules, memory …) read here (ai.info),
// changing nothing. A desktop shows a menu at the button, a phone a sheet.
import { Fragment, h } from "preact";
import { signal } from "@preact/signals";
import { useEffect, useRef, useState } from "preact/hooks";
import type { AIInfo, ChatExport, ChatMsg, MCPProfiles, MdNode, PaletteItem, PaletteList } from "../../../../webproto/proto";
import { copyText } from "../lib/clip";
import { layout } from "../lib/layout";
import { toast } from "../lib/toasts";
import { saveBlob } from "../lib/transfer";
import { ReqError } from "../net/link";
import { link, live, store } from "../session";
import { readChat } from "../state/chat";
import type { StringKey } from "../strings";
import { t } from "../strings";
import { AskLaptop } from "../ui/AskLaptop";
import { Button } from "../ui/Button";
import { errText } from "../ui/CardSheet";
import type { MenuItems } from "../ui/ContextMenu";
import { openMenu } from "../ui/ContextMenu";
import { RowMenu } from "../ui/RowMenu";
import { Sheet } from "../ui/Sheet";

/** The laptop's AI screens, in the order the menu lists them. */
export const INFO_KINDS: readonly [string, StringKey][] = [
  ["usage", "aiinfo.usage"],
  ["rules", "aiinfo.rules"],
  ["memory", "aiinfo.memory"],
  ["skills", "aiinfo.skills"],
  ["hooks", "aiinfo.hooks"],
  ["web-tools", "aiinfo.web_tools"],
  ["mcp-log", "aiinfo.mcp_log"],
  ["serve-status", "aiinfo.serve_status"],
  ["serve-config", "aiinfo.serve_config"],
  ["configure", "aiinfo.configure"],
  ["bug-report", "aiinfo.bug_report"],
];

type Flow =
  | { s: "rename"; id: string; title: string }
  | { s: "fork"; msgs: readonly ChatMsg[] }
  | { s: "delete"; id: string; title: string }
  | { s: "clear" }
  | { s: "laptop"; item: PaletteItem }
  | { s: "info"; kind: string; info: AIInfo | null }
  | { s: "mcp"; list: MCPProfiles }
  | { s: "mcpask"; name: string }
  | { s: "menu"; title: string; items: MenuItems };

/** chatFlow is the open step of More Actions (null: none). */
export const chatFlow = signal<Flow | null>(null);

/** The open chat as More Actions needs it. */
export interface MoreChat {
  id: string;
  title: string;
  msgs: readonly ChatMsg[];
}

/** moreChat is the laptop's open chat (null: not known yet). */
export function moreChat(): MoreChat | null {
  const c = readChat(store.topic("chat").peek()?.data);
  return c ? { id: c.id, title: c.title ?? "", msgs: c.messages ?? [] } : null;
}

function fail(e: unknown): void {
  toast("warn", e instanceof ReqError ? errText(e) : t("cmd.failed"));
}

/** aiCmds are the laptop's AI commands for this page (null: not asked yet). */
let aiCmds: PaletteItem[] | null = null;

/** loadAICmds asks the laptop for its commands once (a viewer gets none). */
function loadAICmds(): Promise<PaletteItem[]> {
  if (aiCmds) return Promise.resolve(aiCmds);
  if (link.meta.peek()?.role !== "controller") return Promise.resolve([]);
  return link.request("palette.list").then(
    (data) => (aiCmds = ((data as PaletteList).items ?? []).filter((it) => it.id.startsWith("ai-"))),
    () => [],
  );
}

/** runLaptop runs a laptop AI command: at once, or after the asks. */
export function runLaptop(it: PaletteItem): void {
  if (it.ask) {
    chatFlow.value = { s: "laptop", item: it };
    return;
  }
  link.request("palette.run", { id: it.id }).then(() => toast("ok", t("cmd.ran_laptop", { name: it.title })), fail);
}

/** openInfo reads one of the laptop's AI screens. */
export function openInfo(kind: string): void {
  chatFlow.value = { s: "info", kind, info: null };
  link.request("ai.info", { kind }).then(
    (data) => {
      const f = chatFlow.peek();
      if (f?.s === "info" && f.kind === kind) chatFlow.value = { s: "info", kind, info: data as AIInfo };
    },
    (e: unknown) => {
      chatFlow.value = null;
      fail(e);
    },
  );
}

/** openMCPProfiles lists the MCP profiles to switch the chat to. */
export function openMCPProfiles(): void {
  link.request("ai.mcp_profiles").then((data) => (chatFlow.value = { s: "mcp", list: data as MCPProfiles }), fail);
}

/** exportChat saves the chat as markdown in the browser. */
export function exportChat(id: string): void {
  link.request("chat.export", { id }).then((data) => {
    const ex = data as ChatExport;
    saveBlob(new Blob([ex.text], { type: "text/markdown" }), ex.name);
    toast("ok", t("chat.exported", { name: ex.name }));
  }, fail);
}

/** startFork lists the chat's questions to fork from. */
export function startFork(msgs: readonly ChatMsg[]): void {
  if (!msgs.some((m) => m.role === "user")) {
    toast("info", t("chat.fork_none"));
    return;
  }
  chatFlow.value = { s: "fork", msgs };
}

/** moreItems are the menu's entries for chat (separators: null). */
function moreItems(chat: MoreChat, cmds: readonly PaletteItem[]): MenuItems {
  const items: (MenuItems[number])[] = [
    { label: t("chat.rename"), run: () => (chatFlow.value = { s: "rename", id: chat.id, title: chat.title }) },
    { label: t("chat.fork"), run: () => startFork(chat.msgs) },
    { label: t("chat.export"), run: () => exportChat(chat.id) },
    { label: t("chat.delete"), run: () => (chatFlow.value = { s: "delete", id: chat.id, title: chat.title }) },
    { label: t("chat.clear"), run: () => (chatFlow.value = { s: "clear" }) },
  ];
  if (cmds.length > 0) items.push(null, ...cmds.map((it) => ({ label: it.title, run: () => runLaptop(it) })));
  items.push(null, { label: t("mcp.switch"), run: openMCPProfiles });
  items.push(null, ...INFO_KINDS.map(([kind, key]) => ({ label: t(key), run: () => openInfo(kind) })));
  return items;
}

/** openMore shows More Actions at the button (a desktop) or as a sheet. */
export function openMore(chat: MoreChat, at: DOMRect): void {
  void loadAICmds().then((cmds) => {
    const items = moreItems(chat, cmds);
    if (layout.peek() === "desktop") openMenu(at.left, at.bottom, t("scm.more_actions"), items);
    else chatFlow.value = { s: "menu", title: t("scm.more_actions"), items };
  });
}

/** plain is a markdown AST's text, on one line. */
function plain(n: MdNode | undefined): string {
  if (!n) return "";
  return (n.text ?? "") + (n.c ?? []).map(plain).join(" ");
}

function TitleForm({ id, title, onDone }: { id: string; title: string; onDone: () => void }) {
  const [v, setV] = useState(title);
  const input = useRef<HTMLInputElement | null>(null);
  useEffect(() => {
    input.current?.focus();
    input.current?.select();
  }, []);
  const ok = v.trim() !== "" && v.trim() !== title && !/[\u0000-\u001f\u007f]/.test(v);
  const send = () => {
    if (!ok || !live.value) return;
    link.request("chat.rename", { id, title: v.trim() }).then(() => {
      toast("ok", t("chat.renamed"));
      onDone();
    }, fail);
  };
  return (
    <Sheet onLater={onDone} label={t("chat.rename")}>
      <form
        class="name-form"
        onSubmit={(e) => {
          e.preventDefault();
          send();
        }}
      >
        <label class="field-label" for="chat-title-input">
          {t("chat.rename_to")}
        </label>
        <input
          id="chat-title-input"
          ref={input}
          class="search-input name-input"
          dir="auto"
          autocomplete="off"
          maxLength={200}
          value={v}
          onInput={(e) => setV((e.currentTarget as HTMLInputElement).value)}
        />
        <div class="choices">
          <button type="button" class="btn btn-neutral" onClick={onDone}>
            {t("cancel")}
          </button>
          <Button kind="primary" needsLink disabled={!ok} onClick={send}>
            {t("files.rename")}
          </Button>
        </div>
      </form>
    </Sheet>
  );
}

function InfoSheet({ info, kind, onDone }: { info: AIInfo | null; kind: string; onDone: () => void }) {
  const name = INFO_KINDS.find(([k]) => k === kind)?.[1];
  return (
    <Sheet onLater={onDone} label={info?.title ?? (name ? t(name) : "")}>
      <section class="ai-info" role="dialog" aria-modal="true" aria-labelledby="ai-info-t">
        <h2 class="ai-info-title" id="ai-info-t" dir="auto">
          {info?.title ?? (name ? t(name) : "")}
        </h2>
        {!info ? (
          <p class="ai-info-note">{t("loading")}</p>
        ) : (
          <>
            {info.note && (
              <p class="ai-info-note" dir="auto">
                {info.note}
              </p>
            )}
            {info.rows.length > 0 && (
              <ul class="ai-info-rows">
                {info.rows.map((r, i) =>
                  r.header ? (
                    <li key={i} class="ai-info-head" dir="auto">
                      {r.title}
                    </li>
                  ) : (
                    <li key={i} class="ai-info-row">
                      <span class="ai-info-label" dir="auto">
                        {r.title}
                      </span>
                      {r.hint && (
                        <span class="ai-info-hint" dir="auto">
                          {r.hint}
                        </span>
                      )}
                    </li>
                  ),
                )}
              </ul>
            )}
            {info.text && (
              <pre class="ai-info-text" dir="ltr">
                {info.text}
              </pre>
            )}
          </>
        )}
        <div class="choices">
          <button type="button" class="btn btn-neutral" onClick={onDone}>
            {t("close")}
          </button>
          {info?.copy && info.text && (
            <button type="button" class="btn btn-primary" onClick={() => copyText(info.text ?? "")}>
              {t("copy")}
            </button>
          )}
          {info?.save && info.text && (
            <button
              type="button"
              class="btn btn-primary"
              onClick={() => {
                saveBlob(new Blob([info.text ?? ""], { type: "text/markdown" }), info.save ?? "report.md");
                toast("ok", t("chat.exported", { name: info.save ?? "" }));
              }}
            >
              {t("file.save")}
            </button>
          )}
        </div>
      </section>
    </Sheet>
  );
}

/** ChatMoreFlows draws the open step of More Actions. */
export function ChatMoreFlows() {
  const f = chatFlow.value;
  const done = () => (chatFlow.value = null);
  if (!f) return null;
  switch (f.s) {
    case "menu":
      return (
        <RowMenu
          title={f.title}
          items={f.items.flatMap((it) => (it ? [{ label: it.label, pick: it.run }] : []))}
          onClose={() => {
            if (chatFlow.peek() === f) done();
          }}
        />
      );
    case "rename":
      return <TitleForm id={f.id} title={f.title} onDone={done} />;
    case "fork": {
      const asks = f.msgs.filter((m) => m.role === "user").reverse();
      return (
        <RowMenu
          title={t("chat.fork")}
          items={asks.map((m) => ({
            label: plain(m.md).trim().slice(0, 80) || m.text?.slice(0, 80) || "#" + m.i,
            pick: () =>
              link.request("chat.fork", { i: m.i }).then(() => toast("ok", t("chat.forked")), (e: unknown) => {
                if (e instanceof ReqError && e.code === "busy") toast("warn", t("chat.new_busy"));
                else fail(e);
              }),
          }))}
          onClose={() => {
            if (chatFlow.peek() === f) done();
          }}
        />
      );
    }
    case "delete":
      return <AskLaptop label={t("chat.delete")} text={t("chat.delete_ask", { title: f.title || t("chat.untitled") })} send={() => link.request("chat.delete", { id: f.id })} onDone={done} />;
    case "clear":
      return <AskLaptop label={t("chat.clear")} text={t("chat.clear_ask")} send={() => link.request("chat.clear")} onDone={done} />;
    case "laptop":
      return <AskLaptop label={f.item.title} text={t("cmd.ask_text", { name: f.item.title })} send={() => link.request("palette.ask", { id: f.item.id })} onDone={done} />;
    case "mcp": {
      const mark = (on: boolean) => (on ? "● " : "");
      const rows = [{ label: mark(f.list.current === "") + t("mcp.all"), name: "" }, ...f.list.profiles.map((p) => ({ label: mark(f.list.current === p.name) + p.name, name: p.name }))];
      return (
        <RowMenu
          title={t("mcp.switch")}
          items={rows.map((r) => ({
            label: r.label,
            pick: () => {
              if (r.name === f.list.current) return void toast("info", t("mcp.in_use"));
              chatFlow.value = { s: "mcpask", name: r.name };
            },
          }))}
          onClose={() => {
            if (chatFlow.peek() === f) done();
          }}
        />
      );
    }
    case "mcpask": {
      const all = f.name === "";
      return (
        <AskLaptop
          label={t("mcp.switch")}
          text={all ? t("mcp.use_all") : t("mcp.use", { name: f.name })}
          send={() => link.request("ai.use_mcp_profile", all ? { all: true } : { name: f.name })}
          onDone={done}
        />
      );
    }
    case "info":
      return <InfoSheet info={f.info} kind={f.kind} onDone={done} />;
  }
}
