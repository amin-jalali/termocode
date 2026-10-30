// Portal draws its children into another element of the page (by id),
// as their own tree, drawn again each time the owner draws. The desktop
// puts a file's editor actions at the end of its editor group's tab bar
// this way, as the laptop's editor does. No such element: the children
// stay where the owner is.
import { Fragment, h, render, type ComponentChildren } from "preact";
import { useLayoutEffect, useState } from "preact/hooks";

export function Portal({ into, children }: { into: string; children: ComponentChildren }) {
  const [el, setEl] = useState<HTMLElement | null>(null);
  useLayoutEffect(() => setEl(document.getElementById(into)));
  useLayoutEffect(() => {
    if (el) render(<Fragment>{children}</Fragment>, el);
  });
  useLayoutEffect(
    () => () => {
      if (el) render(null, el);
    },
    [el],
  );
  return el ? null : <Fragment>{children}</Fragment>;
}
