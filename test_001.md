# Test

## Mouse behaviour (brief)

### Tracking mode
termocode uses xterm **all-motion** mouse reporting (mode 1003) so the UI can react to hover. While a plain text input is focused (prompt, find bar, replace bar, workspace search, or the AI chat input), it drops to **button-only** reporting (mode 1002). This stops split-up mouse escape codes from leaking into typed text. List overlays such as the picker, recents and settings stay in all-motion mode so their rows still highlight on hover. Each of them filters stray mouse input out of its filter field.

### Overlays first
Mouse events go to an open modal before the normal routing runs. This covers preview, search, prompt, confirm, settings, picker, recents and menu. The confirm dialog only reacts to its buttons, so a stray click can't dismiss it. Everything else goes through `handleMouse` in `internal/app/mouse.go`.

### `handleMouse` order (the first area that handles the event wins)
1. **Hover:** every event records the pointer cell for hover highlighting.
2. **Actions launcher chip:** a click re-opens and pins the right-side Actions panel.
3. **Overflow (⋮) menu:** a click on the glyph opens or closes the menu. Hover moves the highlight and a click runs the item. A click outside closes the menu and still reaches whatever is underneath.
4. **AI chat panel:** wheel scrolls; clicking the input focuses it.
5. **Status bar** chips:
   - AI model badge → chat setup / toggle
   - LSP chip → language-server manager
   - error/warning counts → Problems panel
   - tests chip → Testing view
   - debug badge → Run view
   - extension items
6. **Actions panel:** pin and close buttons, plus a drag splitter on its left edge (width limited to half the screen).
7. **Terminal / bottom panel:**
   - **Tab bar buttons:** tabs, close ×, +, maximize, minimize, close panel.
   - **Resizing:** drag any empty cell of the tab bar. Dragging below 3 rows minimizes the panel.
   - **Content:** wheel scrolls the terminal and `file:line` links are clickable. Non-terminal tabs (Output, Problems…) handle their own content.
8. **Find bar:** handles clicks inside its box (↑ ↓ ×). Clicks outside the box pass through.
9. **Sidebar splitter:** drag to resize the explorer.
10. **Activity bar:** a click switches views.
11. **Sidebar:** a left click focuses it.
    - **Files:** click and Ctrl/Shift-click in the tree; right-click opens a context menu.
    - **Git:** clicks, plus a right-click menu.
    - **Run, Tests, extension panels:** each has its own handler. Search has nothing clickable yet.
12. **Editor area:**
    - **Tab bar:** clicks go to the tab bar.
    - **Right-click:** opens the editor context menu.
    - **Welcome screen:** hovering highlights quick-action cards; clicking opens cards, recent files and workspaces.
    - **Ctrl+left-click:** Go to Definition.
    - **Side-by-side diff:** the wheel scrolls both panes together. The ‹ › buttons jump between changes, and hover moves focus to the pane under the pointer.
    - **Everything else:** forwarded to Neovim.

### Drags
Once a splitter drag starts (explorer, Actions panel, terminal), motion events resize the panel. Any other event (release, wheel, right-click) ends the drag and saves the new size.
