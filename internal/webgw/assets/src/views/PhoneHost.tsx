// The page's door to home.js (MOBUX-20): it loads the phone's views on
// first use and draws the one asked for; while loading it shows rows, and
// a failed load can be tried again.
import { h } from "preact";
import { useEffect } from "preact/hooks";
import { home, homeFailed, loadHome } from "../lib/lazy";
import { t } from "../strings";
import { Button } from "../ui/Button";
import { SkeletonRows } from "../ui/Skeleton";
import { SubView } from "./more/SubView";

export function PhoneHost() {
  useEffect(() => {
    loadHome().catch(() => {});
  }, []);
  const m = home.value;
  if (m) return <m.ThisPhone />;
  return (
    <SubView title={t("more.this_phone")} name="phone">
      {homeFailed.value ? (
        <div class="phone-line is-error" role="alert">
          <Button small onClick={() => loadHome().catch(() => {})}>
            {t("retry")}
          </Button>
        </div>
      ) : (
        <div aria-busy="true" aria-label={t("loading")}>
          <SkeletonRows rows={6} mono={false} />
        </div>
      )}
    </SubView>
  );
}
