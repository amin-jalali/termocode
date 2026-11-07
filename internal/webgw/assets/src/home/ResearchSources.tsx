// The sources list of a research answer (MOB-15): one 44 px row per hit
// with its title, site badge and snippet. A tap never opens a link at
// once: it asks first (LinkSheet: the full url, Cancel first). A row
// without a usable url says so instead. Phone layout only; part of the
// Home chunk, never app.js.
import { h } from "preact";
import { askLink } from "../lib/links";
import { RESEARCH_TEXT, sourceURL, type SourceRef } from "./research";

/** SourceItem is a list row of a web_search block (BlockItem shape). */
export interface SourceItem {
  id: string;
  title: string;
  sub?: string;
  badge?: string;
  ref?: SourceRef;
}

export function ResearchSources({ title, items, onNote }: { title: string; items: readonly SourceItem[]; onNote?: (text: string) => void }) {
  return (
    <section class="research-sources" aria-label={title}>
      <ul class="research-list" role="list">
        {items.map((it) => {
          const url = sourceURL(it.ref);
          return (
            <li key={it.id} class="research-row">
              <button
                type="button"
                class="research-hit"
                aria-label={url ? `${RESEARCH_TEXT.open}: ${it.title}` : it.title}
                onClick={() => (url ? askLink(url) : onNote?.(RESEARCH_TEXT.noLink))}
              >
                <span class="research-title" dir="auto">
                  {it.title}
                </span>
                {it.badge && (
                  <span class="research-site mono" dir="ltr">
                    {url ? "↗ " : ""}
                    {it.badge}
                  </span>
                )}
                {it.sub && (
                  <span class="research-sub" dir="auto">
                    {it.sub}
                  </span>
                )}
              </button>
            </li>
          );
        })}
      </ul>
    </section>
  );
}
