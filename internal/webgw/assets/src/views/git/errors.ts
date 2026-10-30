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
