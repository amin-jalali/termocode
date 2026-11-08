// Discard needs a confirm (web UX 3.9): AskLaptop with the question
// "Discard changes in <path>? This cannot be undone.". The page confirms
// the laptop's exact card itself, or (web_approve_on_laptop) it asks the
// laptop and waits. Nothing is discarded before the yes.
import { h } from "preact";
import { link } from "../../session";
import { AskLaptop } from "../../ui/AskLaptop";
import { t } from "../../strings";

export function DiscardFlow({ paths, onDone }: { paths: string[]; onDone: () => void }) {
  return <AskLaptop cls="discard" label={t("git.discard")} text={t("git.discard_body", { path: paths.join(", ") })} send={() => link.request("git.discard", { paths })} onDone={onDone} />;
}
