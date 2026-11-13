// The confirm sheet of a chat link: the full URL in mono, Cancel first and
// focused, Open a real link to a new tab with no opener and no referrer.
// A link to localhost is a port of the computer termocode runs on: it
// offers Forward port instead (a controller only), never the link as it
// is; once forwarded (fwd.ready) the sheet offers the forwarded address.
import { h } from "preact";
import { askForward, loopbackPort } from "../lib/forward";
import { linkAsk, linkReady } from "../lib/links";
import { link } from "../session";
import { t } from "../strings";
import { Sheet } from "./Sheet";

export function LinkSheet({ href }: { href: string }) {
  const close = () => {
    linkAsk.value = null;
    linkReady.value = false;
  };
  const ready = linkReady.value;
  const port = ready ? 0 : loopbackPort(href);
  const controller = link.meta.value?.role === "controller";
  const title = port ? t("fwd.title", { port: String(port) }) : t("link.title");
  const body = t(port ? "fwd.body" : "link.body");
  return (
    <Sheet onLater={close} label={title}>
      <section class="card card-dialog link-ask" role="dialog" aria-modal="true" aria-labelledby="link-ask-t">
        <header class="card-head">
          <h2 class="card-title" id="link-ask-t">
            {title}
          </h2>
        </header>
        <pre class="card-subject" dir="ltr">
          {href}
        </pre>
        <p class="card-body">{body}</p>
        <footer class="card-foot">
          <div class="choices">
            <button type="button" class="btn btn-neutral" onClick={close}>
              {t("cancel")}
            </button>
            {port ? (
              controller && (
                <button
                  type="button"
                  class="btn btn-primary"
                  onClick={() => {
                    close();
                    askForward(href);
                  }}
                >
                  {t("fwd.forward")}
                </button>
              )
            ) : (
              <a class="btn btn-primary" href={href} target="_blank" rel="noopener noreferrer" onClick={close}>
                {t("link.open")}
              </a>
            )}
          </div>
        </footer>
      </section>
    </Sheet>
  );
}
