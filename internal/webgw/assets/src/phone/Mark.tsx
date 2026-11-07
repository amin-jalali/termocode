// StatusMark draws a status as its shape in its colour, with the status
// said to screen readers (or hidden when the text next to it says it).
import { h } from "preact";
import { t } from "../strings";
import { STATUS, type Status } from "./status";

export function StatusMark({ s, live, hidden }: { s: Status; live?: boolean; hidden?: boolean }) {
  const look = STATUS[s];
  return (
    <span
      class={`ph-st ph-st-${look.tone}${live ? " is-live" : ""}`}
      role={hidden ? undefined : "img"}
      aria-label={hidden ? undefined : t(look.label)}
      aria-hidden={hidden ? "true" : undefined}
    >
      {look.glyph}
    </span>
  );
}
