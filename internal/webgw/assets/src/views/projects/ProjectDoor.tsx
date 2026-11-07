// The phone's Open / Close for a project row (MOB-51), for the projects
// list (MOBUX-6). Open: an in-page sheet first (Cancel first, the yes
// arms after 600 ms), then "Waiting for the laptop…" until the laptop's
// card is answered. Close: asks in the page first (Cancel first), only for a project the phone opened; a
// project opened on the laptop says "Close it there". English only.
import { h } from "preact";
import { useState } from "preact/hooks";
import type { ProjectItem } from "../../../../../webproto/proto";
import type { Door } from "../../net/door";
import { toast } from "../../lib/toasts";
import { link } from "../../session";
import { askHere } from "../../ui/AskHere";
import { AskLaptop } from "../../ui/AskLaptop";
import { Button } from "../../ui/Button";
import { canOpen, closeProject, projectText as tx } from "./door";

interface Props {
  p: ProjectItem;
  door: Door;
}

export function ProjectDoor({ p, door }: Props) {
  const [ask, setAsk] = useState(false);
  const [closing, setClosing] = useState(false);
  if (p.state === "opening") {
    return (
      <span class="proj-state" role="status">
        ◐ {tx.opening}
      </span>
    );
  }
  if (canOpen(p)) {
    return (
      <span class="proj-door">
        <span class="proj-state">○ {tx.asleep}</span>
        <Button small needsLink label={tx.openLabel(p.name)} onClick={() => setAsk(true)}>
          {tx.open}
        </Button>
        {ask && (
          <AskLaptop
            label={tx.openLabel(p.name)}
            text={tx.openQ(p.name)}
            send={() => link.request("project.open", { ws: p.ws })}
            onDone={() => setAsk(false)}
          />
        )}
      </span>
    );
  }
  if (p.state !== "running" || !p.ws) return null;
  const close = () => {
    if (closing) return;
    askHere.value = { label: `${tx.close} ${p.name}`, text: tx.closeQ(p.name), verb: tx.close, run: doClose };
  };
  const doClose = () => {
    setClosing(true);
    closeProject(door, p.ws as string).then(
      () => toast("ok", `${p.name} is closing.`),
      (e: unknown) => {
        toast("info", e instanceof Error ? e.message : String(e));
        setClosing(false);
      },
    );
  };
  return (
    <Button small needsLink disabled={closing} label={`${tx.close} ${p.name}`} onClick={close}>
      {tx.close}
    </Button>
  );
}
