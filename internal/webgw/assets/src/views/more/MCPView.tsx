// MCP server (WEB-45, web design 2.9): termocode as an MCP server for
// outside agents, as the laptop's status screen shows it: listening or
// why not, the agents connected now and their last calls. Read only: no
// start, stop, disconnect or allow from here. #/more/mcp
import { h } from "preact";
import { store } from "../../session";
import { hhmm, readMCP } from "../../state/more";
import { t } from "../../strings";
import { TopicBody } from "../Placeholder";
import { SubView } from "./SubView";

function MCPBody() {
  const v = readMCP(store.topic("mcp").value?.data);
  return (
    <div class="mcpv">
      <p class={`mcp-state${v.listening ? " is-on" : ""}`} role="status">
        <span class="mcp-dot" aria-hidden="true" />
        <span>{v.listening ? t("mcp.listening") : t("mcp.not_listening")}</span>
      </p>
      {!v.listening && v.why && <p class="mcp-why">{v.why}</p>}
      <section class="group" aria-labelledby="mcp-agents">
        <h2 class="group-title" id="mcp-agents">
          {t("mcp.agents")}
        </h2>
        {v.agents.length === 0 ? (
          <p class="empty">{t("mcp.no_agents")}</p>
        ) : (
          <ul class="mcp-list">
            {v.agents.map((a, i) => (
              <li class="mcp-agent" key={i}>
                <span class="mcp-name" dir="auto">
                  {a.name || t("mcp.no_name")}
                </span>
                <span class="mcp-meta" dir="ltr">
                  {t("mcp.agent_meta", { time: hhmm(a.since), calls: a.calls, asked: a.asked })}
                </span>
              </li>
            ))}
          </ul>
        )}
      </section>
      <section class="group" aria-labelledby="mcp-calls">
        <h2 class="group-title" id="mcp-calls">
          {t("mcp.calls")}
        </h2>
        {v.calls.length === 0 ? (
          <p class="empty">{t("mcp.no_calls")}</p>
        ) : (
          <ul class="mcp-list mcp-calls">
            {v.calls.map((c, i) => (
              <li class="mcp-call" key={i} dir="ltr">
                {c}
              </li>
            ))}
          </ul>
        )}
      </section>
      <p class="view-note">{t("mcp.readonly")}</p>
    </div>
  );
}

export function MCPView() {
  return (
    <SubView title={t("more.mcp")}>
      <TopicBody topic="mcp" empty={<p class="empty">{t("mcp.not_listening")}</p>}>
        <MCPBody />
      </TopicBody>
    </SubView>
  );
}
