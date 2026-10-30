// A git editor tab (desktop, lib/gittab.ts): the commit graph, a merge
// editor, an interactive rebase, the pull requests or one of them.
import { h } from "preact";
import { fileOfTab } from "../../lib/difftab";
import { gitTabIcon, gitTabOf } from "../../lib/gittab";
import { Cod, FileIcon } from "../../ui/icons";
import { GraphView } from "./GraphView";
import { MergeEditor } from "./MergeEditor";
import { PRDetailView, PRListView } from "./PRViews";
import { RebaseView } from "./RebaseView";

export function GitTabView({ tab, slot }: { tab: string; slot?: string }) {
  const g = gitTabOf(tab);
  switch (g?.kind) {
    case "graph":
      return <GraphView />;
    case "rebase":
      return <RebaseView hash={g.hash} />;
    case "merge":
      return <MergeEditor path={g.path} slot={slot} />;
    case "prs":
      return <PRListView />;
    case "pr":
      return <PRDetailView n={g.n} />;
  }
  return null;
}

/** TabIcon is an editor tab's icon: a git view's codicon, else the file's. */
export function TabIcon({ path }: { path: string }) {
  const g = gitTabOf(path);
  if (g) return <span class="ficon ficon-cod" aria-hidden="true"><Cod name={gitTabIcon(g)} /></span>;
  return <FileIcon path={fileOfTab(path) ?? path} />;
}
