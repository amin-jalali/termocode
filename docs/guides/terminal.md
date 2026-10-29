# The terminal and its splits

The terminal panel sits under the editor. Press `Ctrl+T` to show or hide it.
Hiding it does not stop your shells.

A terminal **tab** can hold up to four **panes**: shells side by side or
stacked. Each pane is a full terminal.

## Split a tab

- `Alt+/` splits the focused pane to the right (**Terminal: Split Right**).
- `Alt+-` splits it downwards (**Terminal: Split Down**).

The new shell starts in the same folder as the pane you split.

A pane needs at least 20 columns and 4 rows. If the panel is too small,
termocode shows a hint instead of making a pane you cannot use. Make the
panel bigger, or split the other way.

## Move between panes

- Click a pane to focus it.
- `Alt+Left` and `Alt+Right` move to the previous or next pane
  (**Terminal: Focus Previous Pane**, **Terminal: Focus Next Pane**).

The focused pane has a bright title bar. The other panes are dim.

## Resize

Drag the divider between two panes with the mouse. Or use
**Terminal: Grow Pane** and **Terminal: Shrink Pane** from the palette.

## Close a pane

Run **Terminal: Close Pane**. This stops the shell in that pane. Closing the
last pane closes the tab.

When a shell ends (for example you type `exit`), its pane stays and says
`[exited · Enter to restart · x to close]`. Press `Enter` to start a new shell
there, or `x` to close the pane.

## Good to know

- Switching to another tab hides its panes. The shells keep running.
- Terminal tabs and panes are not restored after you quit termocode.
