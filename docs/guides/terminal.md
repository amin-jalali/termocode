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

## After a restart

When you detach (over SSH, or when the connection drops), your shells keep
running. When you quit and start termocode again, the shells end, but your
terminal tabs come back:

- each tab and each pane, split the same way,
- each shell in the folder it was in (with shell integration, the folder you
  last moved to with `cd`),
- the same tab and pane in focus, and the panel open or hidden as before.

Each shell is new: output, scrollback and environment values are not kept.

- A pane that was running a program says so in its title bar, for example
  `was running: npm`. Only the program name is kept.
- If the folder is gone, the shell starts in the project folder and the pane
  says so.
- The note goes away when you type in the pane.

To run the last command again with `Enter`, set
`"terminal_restore_commands": true` in `config.json`. Then termocode also keeps
the command line, and the pane shows `was running: npm run dev · Enter to run
it again`. It is off by default, because a command line can hold a secret.

To start with an empty panel every time, set `"terminal_restore": false`.

## Good to know

- Switching to another tab hides its panes. The shells keep running.
- Tabs started by the task runner or the Services view are not restored.
