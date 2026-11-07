// #/more/tools: the "All tools" menu on the phone (MOBUX-5). It reads
// the laptop's view.catalog when it opens (and with Try again); a row
// opens the phone screen of a feature or the view in the Home.
import { h } from "preact";
import { useEffect, useState } from "preact/hooks";
import { go } from "../lib/router";
import { link } from "../session";
import { t } from "../strings";
import { SubView } from "../views/more/SubView";
import { AllTools, type CatalogState } from "./AllTools";
import { readCatalog, viewHash, type Target } from "./alltools";

/** openTarget goes to a row's target. */
export function openTarget(target: Target): void {
  if ("route" in target) go(target.route);
  else location.hash = viewHash(target);
}

export function AllToolsView() {
  const [cat, setCat] = useState<CatalogState>({ state: "loading" });
  const [n, setN] = useState(0);
  useEffect(() => {
    let gone = false;
    setCat({ state: "loading" });
    link.request("view.catalog", {}).then(
      (r) => !gone && setCat({ state: "ok", catalog: readCatalog(r) }),
      () => !gone && setCat({ state: "failed" }),
    );
    return () => {
      gone = true;
    };
  }, [n]);
  return (
    <SubView title={t("tools.title")} name="tools">
      <AllTools cat={cat} onRetry={() => setN((x) => x + 1)} onOpen={openTarget} />
    </SubView>
  );
}
