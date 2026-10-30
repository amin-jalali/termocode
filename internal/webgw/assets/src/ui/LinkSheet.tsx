// The confirm sheet of a chat link: the full URL in mono, Cancel first and
// focused, Open a real link to a new tab with no opener and no referrer.
import { h } from "preact";
import { linkAsk } from "../lib/links";
import { t } from "../strings";
import { Sheet } from "./Sheet";

export function LinkSheet({ href }: { href: string }) {
  const close = () => (linkAsk.value = null);
  return (
    <Sheet onLater={close} label={t("link.title")}>
      <section class="card card-dialog link-ask" role="dialog" aria-modal="true" aria-labelledby="link-ask-t">
        <header class="card-head">
          <h2 class="card-title" id="link-ask-t">
            {t("link.title")}
          </h2>
        </header>
        <pre class="card-subject" dir="ltr">
          {href}
        </pre>
        <p class="card-body">{t("link.body")}</p>
        <footer class="card-foot">
          <div class="choices">
            <button type="button" class="btn btn-neutral" onClick={close}>
              {t("cancel")}
            </button>
            <a class="btn btn-primary" href={href} target="_blank" rel="noopener noreferrer" onClick={close}>
              {t("link.open")}
            </a>
          </div>
        </footer>
      </section>
    </Sheet>
  );
}
