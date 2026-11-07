// The "All tools" menu (MOBUX-5, ☰): one searchable list of every
// built-in feature, every MCP server (tap: its tools) and every saved
// view; each row opens its view. Phone layout: one column, 44 px rows,
// the search field on top. The features show even when the laptop's
// catalog cannot be read (then a line with Try again).
import { h, type ComponentChildren } from "preact";
import { useState } from "preact/hooks";
import { t } from "../strings";
import { Button } from "../ui/Button";
import { SkeletonRows } from "../ui/Skeleton";
import {
  builtinViews,
  FEATURES,
  filterRanked,
  queryWords,
  savedTarget,
  viewTarget,
  type Catalog,
  type CatalogView,
  type Feature,
  type MCPServer,
  type SavedView,
  type Target,
} from "./alltoolsdata";

/** CatalogState is how far the laptop's catalog is. */
export type CatalogState = { state: "loading" } | { state: "ok"; catalog: Catalog } | { state: "failed" };

function Row({ id, name, sub, mark, onClick }: { id: string; name: string; sub?: string; mark?: string; onClick: () => void }) {
  return (
    <li>
      <button type="button" class="tools-row" data-tool={id} onClick={onClick}>
        {mark && (
          <span class="tools-mark" aria-hidden="true">
            {mark}
          </span>
        )}
        <span class="tools-text">
          <span class="tools-name" dir="auto">
            {name}
          </span>
          {sub && (
            <span class="tools-sub" dir="auto">
              {sub}
            </span>
          )}
        </span>
      </button>
    </li>
  );
}

function Group({ id, title, children }: { id: string; title: string; children: ComponentChildren }) {
  return (
    <section class="tools-group" data-group={id} aria-labelledby={`tools-${id}`}>
      <h2 class="group-title" id={`tools-${id}`}>
        {title}
      </h2>
      {children}
    </section>
  );
}

function toolRow(v: CatalogView, onOpen: (t: Target) => void, withServer: boolean) {
  const name = withServer && v.server ? `${v.server} · ${v.title}` : v.title;
  const sub = v.run ? v.description : t("tools.ask_ai");
  return <Row key={v.id} id={v.id} name={name} sub={sub} mark={v.run ? undefined : "✦"} onClick={() => onOpen(viewTarget(v))} />;
}

/** ServerTools is one MCP server's tools and resources, drilled in. */
function ServerTools({ server, onBack, onOpen }: { server: MCPServer; onBack: () => void; onOpen: (t: Target) => void }) {
  return (
    <div class="tools-server" data-server={server.name}>
      <button type="button" class="sub-back tools-back" onClick={onBack}>
        ‹ {t("back")}
      </button>
      <Group id="server" title={server.name}>
        {server.tools.length === 0 ? (
          <p class="empty">{t("tools.server_empty")}</p>
        ) : (
          <ul class="tools-list">{server.tools.map((v) => toolRow(v, onOpen, false))}</ul>
        )}
      </Group>
    </div>
  );
}

/** AllTools is the menu itself; the caller reads the catalog and says
 * where a target goes. */
export function AllTools({ cat, onRetry, onOpen }: { cat: CatalogState; onRetry: () => void; onOpen: (t: Target) => void }) {
  const [query, setQuery] = useState("");
  const [server, setServer] = useState("");
  const c = cat.state === "ok" ? cat.catalog : null;
  const open = c && server ? c.servers.find((s) => s.name === server) : undefined;
  if (open) return <ServerTools server={open} onBack={() => setServer("")} onOpen={onOpen} />;

  const words = queryWords(query);
  const searching = words.length > 0;
  const features = filterRanked(words, FEATURES, (f: Feature) => t(f.label), (f) => `${t(f.label)} ${t(f.views)} ${f.id} ${f.words}`);
  // A search also finds the features' own views of the catalog.
  const featureViews = searching && c ? filterRanked(words, builtinViews(c), (v) => v.title, (v) => `${v.title} ${v.id} ${v.feature}`) : [];
  const servers = c ? filterRanked(words, c.servers, (s) => s.name, (s) => s.name) : [];
  const tools = searching && c ? filterRanked(words, c.servers.flatMap((s) => s.tools), (v) => v.title, (v) => `${v.server ?? ""} ${v.title} ${v.tool ?? ""} ${v.description ?? ""}`) : [];
  const saved = c ? filterRanked(words, c.saved, (s: SavedView) => s.name, (s) => `${s.name} ${s.view}`) : [];
  const nothing = searching && !features.length && !featureViews.length && !servers.length && !tools.length && !saved.length;

  return (
    <div class="tools">
      <div class="tools-search">
        <input
          class="search-input tools-input"
          type="search"
          dir="auto"
          value={query}
          aria-label={t("tools.search")}
          placeholder={t("tools.search")}
          onInput={(e) => setQuery((e.currentTarget as HTMLInputElement).value)}
        />
      </div>
      {nothing && (
        <p class="empty tools-nothing" role="status">
          {t("tools.none", { query: query.trim() })}
        </p>
      )}
      {(features.length > 0 || featureViews.length > 0) && (
        <Group id="features" title={t("tools.features")}>
          <ul class="tools-list">
            {features.map((f) => (
              <Row key={f.id} id={f.id} name={t(f.label)} sub={t(f.views)} onClick={() => onOpen({ route: f.route })} />
            ))}
            {featureViews.map((v) => toolRow(v, onOpen, false))}
          </ul>
        </Group>
      )}
      {cat.state === "loading" && (
        <Group id="mcp" title={t("tools.mcp")}>
          <SkeletonRows rows={3} mono={false} />
        </Group>
      )}
      {cat.state === "failed" && !searching && (
        <div class="tools-failed" role="status">
          <p class="empty">{t("tools.unreachable")}</p>
          <Button kind="neutral" small onClick={onRetry}>
            {t("retry")}
          </Button>
        </div>
      )}
      {c && (!searching || servers.length > 0 || tools.length > 0) && (
        <Group id="mcp" title={t("tools.mcp")}>
          {c.servers.length === 0 ? (
            <p class="empty">{t("tools.mcp_none")}</p>
          ) : (
            <ul class="tools-list">
              {servers.map((s) => (
                <Row
                  key={s.name}
                  id={"server:" + s.name}
                  name={s.name}
                  sub={t("tools.server_meta", { tools: s.tools.length - s.resources, resources: s.resources })}
                  onClick={() => setServer(s.name)}
                />
              ))}
              {tools.map((v) => toolRow(v, onOpen, true))}
            </ul>
          )}
        </Group>
      )}
      {c && (!searching || saved.length > 0) && (
        <Group id="saved" title={t("tools.saved")}>
          {c.saved.length === 0 ? (
            <p class="empty">{t("tools.saved_none")}</p>
          ) : (
            <ul class="tools-list">
              {saved.map((s) => (
                <Row key={s.id} id={"saved:" + s.id} name={s.name} sub={s.pinned ? t("tools.pinned") : s.view} mark={s.pinned ? "📌" : undefined} onClick={() => onOpen(savedTarget(s))} />
              ))}
            </ul>
          )}
        </Group>
      )}
    </div>
  );
}
