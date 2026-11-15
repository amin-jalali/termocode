// The phone's Git tab (home.js): it loads the Git pages (gitphone.js,
// lib/gitphone.ts) on first open and draws them; while loading it shows
// rows, and a failed load can be tried again.
import { h } from "preact";
import { useEffect } from "preact/hooks";
import { gitPhone, gitPhoneFailed, loadGitPhone } from "../../lib/gitphone";
import { t } from "../../strings";
import { SkeletonRows } from "../../ui/Skeleton";
import { View } from "../View";

export function GitPhoneView() {
  useEffect(() => {
    loadGitPhone().catch(() => undefined);
  }, []);
  const m = gitPhone.value;
  if (m) return <m.GitPage />;
  return (
    <View tab="git" title={t("tab.git")}>
      {gitPhoneFailed.value ? (
        <button type="button" class="btn btn-neutral" onClick={() => loadGitPhone().catch(() => undefined)}>
          {t("retry")}
        </button>
      ) : (
        <SkeletonRows rows={6} />
      )}
    </View>
  );
}
