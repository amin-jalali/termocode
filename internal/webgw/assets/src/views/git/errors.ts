// What a failed git request shows: the hub's own words (short English
// for the user), or "Offline" when the link is down.
import { OfflineError, ReqError } from "../../net/link";
import { toast } from "../../lib/toasts";
import { t } from "../../strings";

export function gitFailed(e: unknown): void {
  if (e instanceof OfflineError) toast("warn", t("offline.input"));
  else if (e instanceof ReqError) toast("error", e.message);
  else toast("error", t("err.internal"));
}

/** gitErrText is the same words for a view to show in place. */
export function gitErrText(e: unknown): string {
  if (e instanceof OfflineError) return t("offline.input");
  if (e instanceof ReqError) return e.message;
  return t("err.internal");
}
