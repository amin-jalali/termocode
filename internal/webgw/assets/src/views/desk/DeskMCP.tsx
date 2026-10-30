// The desktop's MCP view: termocode's MCP server for outside agents as a
// list like the laptop editor's MCP servers: the server with its state's
// dot (listening, or why not), the connected agents with their calls, and
// the last calls (tool, agent, what happened, when). An agent opens its
// details in an editor tab. Read only: start, stop and allow stay on the
// laptop.
import { h } from "preact";
import { signal } from "@preact/signals";
import { useEffect } from "preact/hooks";
import { mcpTab } from "../../lib/vtabs";
import { showFile } from "../../lib/workbench";
import { link, store } from "../../session";
import { hhmm, readMCP, type MCPAgentView } from "../../state/more";
import { t } from "../../strings";
import { Cod } from "../../ui/icons";
import { PaneSection, PaneView, Row, Tree } from "../../ui/pane";

/** Call is one line of the hub's call log: "12:03 agent · tool · outcome". */
export interface Call {
  time: string;
  agent: string;
  tool: string;
  outcome: string;
  raw: string;
}

/** readCall splits a call line (a line it cannot split stays whole). */
export function readCall(raw: string): Call {
  const m = /^(\d{1,2}:\d{2})\s+(.*)$/.exec(raw);
  const rest = m ? m[2] : raw;
  const parts = rest.split(" · ");
  if (parts.length >= 3) return { time: m ? m[1] : "", agent: parts[0], tool: parts[1], outcome: parts.slice(2).join(" · "), raw };
  return { time: m ? m[1] : "", agent: "", tool: rest, outcome: "", raw };
}

const shut = signal<ReadonlySet<string>>(new Set());
function toggle(id: string): void {
  const n = new Set(shut.peek());
  if (n.has(id)) n.delete(id);
  else n.add(id);
  shut.value = n;
}

function asked(c: Call): boolean {
  return /^asked/i.test(c.outcome.trim());
}

function CallRow({ c, level }: { c: Call; level: number }) {
  return (
    <Row
      level={level}
      icon={<span class={asked(c) ? "c-warn" : "c-ok"}>{<Cod name={asked(c) ? "shield" : "check"} />}</span>}
      label={c.tool}
      desc={[c.agent, c.outcome].filter(Boolean).join(" · ")}
      title={c.raw}
      end={c.time && <span class="tr-age">{c.time}</span>}
    />
  );
}

export function DeskMCP() {
  useEffect(() => link.subscribe("mcp"), []);
  const st = store.topic("mcp").value;
  const v = readMCP(st?.data);
  const calls = v.calls.map(readCall);
  return (
    <PaneView id="mcp" title={t("more.mcp")}>
      <div class="pv-scroll">
        <Tree label={t("more.mcp")}>
          <Row
            level={0}
            icon={<span class={`mcp-dot${v.listening ? " is-on" : ""}`} aria-hidden="true" />}
            label="termocode"
            desc={!st ? t("loading") : v.listening ? t("mcpv.running") : v.why || t("mcp.not_listening")}
            title={v.listening ? t("mcp.listening") : v.why || t("mcp.not_listening")}
            data={{ server: "termocode" }}
            end={st && <span class="count" title={t("mcp.agents")}>{v.agents.length}</span>}
          />
        </Tree>
        <PaneSection id="agents" title={t("mcp.agents")} open={!shut.value.has("agents")} onToggle={() => toggle("agents")} count={v.agents.length} fixed>
          {v.agents.length === 0 ? (
            <p class="pv-msg">{t("mcp.no_agents")}</p>
          ) : (
            <Tree label={t("mcp.agents")}>
              {v.agents.map((a: MCPAgentView, i) => (
                <Row
                  key={i}
                  level={0}
                  icon={<Cod name="person" />}
                  label={a.name || t("mcp.no_name")}
                  desc={t("mcp.agent_meta", { time: hhmm(a.since), calls: a.calls, asked: a.asked })}
                  data={{ agent: a.name }}
                  onClick={() => showFile(mcpTab(a.name), true)}
                />
              ))}
            </Tree>
          )}
        </PaneSection>
        <PaneSection id="calls" title={t("mcp.calls")} open={!shut.value.has("calls")} onToggle={() => toggle("calls")} count={calls.length} fixed>
          {calls.length === 0 ? (
            <p class="pv-msg">{t("mcp.no_calls")}</p>
          ) : (
            <Tree label={t("mcp.calls")}>
              {calls.map((c, i) => (
                <CallRow key={i} c={c} level={0} />
              ))}
            </Tree>
          )}
        </PaneSection>
        <p class="pv-msg">{t("mcp.readonly")}</p>
      </div>
    </PaneView>
  );
}

/** MCPAgentTab is an outside agent's details in an editor tab. */
export function MCPAgentTab({ name }: { name: string; slot: string }) {
  useEffect(() => link.subscribe("mcp"), []);
  const v = readMCP(store.topic("mcp").value?.data);
  const a = v.agents.find((x) => x.name === name);
  const calls = v.calls.map(readCall).filter((c) => c.agent === name);
  return (
    <section class="view mcpd" data-view="mcp-agent" aria-labelledby="mcpd-h">
      <header class="mcpd-head">
        <span class="mcpd-icon">
          <Cod name="person" />
        </span>
        <div>
          <h1 id="mcpd-h">{name || t("mcp.no_name")}</h1>
          <p class="mcpd-sub">
            <span class={`mcp-dot${a ? " is-on" : ""}`} aria-hidden="true" /> {a ? t("mcpv.connected", { time: hhmm(a.since) }) : t("mcpv.gone")}
          </p>
          {a && <p class="mcpd-sub">{t("mcpv.counts", { calls: a.calls, asked: a.asked })}</p>}
        </div>
      </header>
      <h2 class="mcpd-title">{t("mcp.calls")}</h2>
      {calls.length === 0 ? (
        <p class="mcpd-empty">{t("mcp.no_calls")}</p>
      ) : (
        <table class="mcpd-table">
          <thead>
            <tr>
              <th scope="col">{t("mcpv.time")}</th>
              <th scope="col">{t("mcpv.tool")}</th>
              <th scope="col">{t("mcpv.outcome")}</th>
            </tr>
          </thead>
          <tbody>
            {calls.map((c, i) => (
              <tr key={i}>
                <td dir="ltr">{c.time}</td>
                <td dir="ltr">{c.tool}</td>
                <td>{c.outcome}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      <p class="mcpd-empty">{t("mcp.readonly")}</p>
    </section>
  );
}

