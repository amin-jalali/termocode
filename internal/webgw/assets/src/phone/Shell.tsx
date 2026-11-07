// The phone app shell (MOBUX-2): one column the height of the screen.
//   top bar   project switcher ▾ · live mark · ☰ (All tools)
//   stream    the chat: messages and blocks, newest at the bottom
//   dock      the chip row, then the composer (attach chips, +, 🎤, Send)
// The dock is the one-hand thumb zone: everything the thumb does often
// (chips, typing, Send) is at the bottom; the top bar holds what is used
// now and then. The shell draws only what its host hands it (the Home
// chunk, MOB-4, or the kit): no session, link or store of its own.
// Phone layout only; the desktop panel never draws it.
import { h, type ComponentChildren } from "preact";
import { useLayoutEffect, useRef, useState } from "preact/hooks";
import { t } from "../strings";
import { Composer, type Attached } from "./Composer";
import { PhoneSheet } from "./Sheet";

/** Link is what the live mark shows: always a shape and a word. */
export type LinkState = "live" | "offline" | "asleep";

const LINK_LOOK: Readonly<Record<LinkState, { glyph: string; tone: string; label: "ph.live" | "ph.offline" | "ph.asleep" }>> = {
  live: { glyph: "●", tone: "done", label: "ph.live" },
  offline: { glyph: "✗", tone: "failed", label: "ph.offline" },
  asleep: { glyph: "◌", tone: "asleep", label: "ph.asleep" },
};

export interface ShellProject {
  id: string;
  /** name is the project's short name (data). */
  name: string;
  /** where is a ~-relative folder shown under the name (data). */
  where?: string;
}

/** ProjectSwitcher is the sheet of projects; the current one is marked
 * ✓ and aria-current. Picking the current one just closes it. */
export function ProjectSwitcher({
  projects,
  current,
  onPick,
  onLater,
}: {
  projects: readonly ShellProject[];
  current: string;
  onPick: (id: string) => void;
  onLater: () => void;
}) {
  return (
    <PhoneSheet title={t("ph.switch_title")} onLater={onLater}>
      <ul class="ph-switch-list" role="list">
        {projects.map((p) => {
          const on = p.id === current;
          return (
            <li key={p.id}>
              <button
                type="button"
                class={`ph-switch-row${on ? " is-on" : ""}`}
                aria-current={on ? "true" : undefined}
                autoFocus={on}
                onClick={() => (on ? onLater() : onPick(p.id))}
              >
                <span class="ph-switch-main">
                  <span class="ph-switch-name" dir="auto">
                    {p.name}
                  </span>
                  {p.where && (
                    <span class="ph-switch-where" dir="auto">
                      {p.where}
                    </span>
                  )}
                </span>
                {on && (
                  <span class="ph-switch-check" role="img" aria-label={t("ph.current")}>
                    ✓
                  </span>
                )}
              </button>
            </li>
          );
        })}
      </ul>
    </PhoneSheet>
  );
}

/** TopBar: the project switcher on the start side, the live mark and ☰
 * on the end side. Without onSwitch (one project) the name is plain. */
export function TopBar({ project, link, onSwitch, onMenu }: { project: string; link: LinkState; onSwitch?: () => void; onMenu: () => void }) {
  const look = LINK_LOOK[link];
  return (
    <header class="ph-top">
      {onSwitch ? (
        <button type="button" class="ph-switch" aria-haspopup="dialog" aria-label={t("ph.switch", { name: project })} onClick={onSwitch}>
          <span class="ph-switch-label" dir="auto">
            {project}
          </span>
          <span class="ph-switch-caret" aria-hidden="true">
            ▾
          </span>
        </button>
      ) : (
        <span class="ph-switch is-plain" dir="auto">
          {project}
        </span>
      )}
      <span class={`ph-live tone-${look.tone}`} role="status">
        <span class={`ph-st ph-st-${look.tone}`} aria-hidden="true">
          {look.glyph}
        </span>
        <span class="ph-live-word">{t(look.label)}</span>
      </span>
      <button type="button" class="ph-top-btn is-menu" aria-label={t("ph.menu")} aria-haspopup="dialog" onClick={onMenu}>
        <span aria-hidden="true">☰</span>
      </button>
    </header>
  );
}

const NEAR_END_PX = 48;

/** Stream is the chat's scroll area. It stays at its end while the
 * reader is there (a new message or block scrolls into view) and keeps
 * still while they read older text. */
export function Stream({ children, label }: { children: ComponentChildren; label?: string }) {
  const el = useRef<HTMLDivElement | null>(null);
  const stick = useRef(true);
  useLayoutEffect(() => {
    const s = el.current;
    if (s && stick.current) s.scrollTop = s.scrollHeight;
  });
  return (
    <div
      ref={el}
      class="ph-stream"
      role="log"
      aria-live="polite"
      aria-label={label ?? t("ph.stream")}
      tabIndex={0}
      onScroll={(e) => {
        const s = e.currentTarget as HTMLDivElement;
        stick.current = s.scrollHeight - s.scrollTop - s.clientHeight < NEAR_END_PX;
      }}
    >
      {children}
    </div>
  );
}

/** StreamMsg is one chat message in the stream: the user's on the end
 * side, the AI's full width (its blocks need the room). */
export function StreamMsg({ from, children }: { from: "you" | "ai"; children: ComponentChildren }) {
  return <div class={`ph-msg is-${from}`}>{children}</div>;
}

export interface ShellComposer {
  value: string;
  onInput: (v: string) => void;
  onSend: () => void;
  attached?: readonly Attached[];
  onRemove?: (id: string) => void;
  onAttach?: () => void;
}

/** PhoneShell puts the bar, the stream and the dock together. chips is
 * the chip row (MOBUX-3's ChipRow or PhoneChips); children are the
 * stream's messages and blocks. */
export function PhoneShell({
  project,
  projects,
  onProject,
  link,
  onMenu,
  chips,
  composer,
  children,
}: {
  project: ShellProject;
  /** projects lists every project the switcher offers (two or more show ▾). */
  projects?: readonly ShellProject[];
  onProject?: (id: string) => void;
  link: LinkState;
  onMenu: () => void;
  chips?: ComponentChildren;
  composer: ShellComposer;
  children: ComponentChildren;
}) {
  const [switching, setSwitching] = useState(false);
  const many = !!projects && projects.length > 1 && !!onProject;
  return (
    <div class={`ph-shell${link !== "live" ? " is-offline" : ""}`}>
      <TopBar project={project.name} link={link} onSwitch={many ? () => setSwitching(true) : undefined} onMenu={onMenu} />
      <Stream>{children}</Stream>
      <div class="ph-dock">
        {chips}
        <Composer
          value={composer.value}
          onInput={composer.onInput}
          onSend={composer.onSend}
          attached={composer.attached}
          onRemove={composer.onRemove}
          onAttach={composer.onAttach}
          offline={link !== "live"}
          mic
        />
      </div>
      {switching && many && projects && onProject && (
        <ProjectSwitcher
          projects={projects}
          current={project.id}
          onLater={() => setSwitching(false)}
          onPick={(id) => {
            setSwitching(false);
            onProject(id);
          }}
        />
      )}
    </div>
  );
}
