// Navigation: the bottom tab bar on phones, the rail on tablets and
// desktops (web UX 4.1–4.3). Same five items, labels always visible,
// badges in reserved space so nothing shifts when they appear.
import { h, type ComponentType } from "preact";
import { goTab, route } from "../lib/router";
import { status } from "../session";
import type { Tab } from "../state/route";
import { t, type StringKey } from "../strings";
import { IconChat, IconFiles, IconGit, IconMore, IconTerm } from "./icons";

interface Item {
  tab: Tab;
  label: StringKey;
  Icon: ComponentType;
}

export const NAV_ITEMS: readonly Item[] = [
  { tab: "chat", label: "tab.chat", Icon: IconChat },
  { tab: "files", label: "tab.files", Icon: IconFiles },
  { tab: "term", label: "tab.term", Icon: IconTerm },
  { tab: "git", label: "tab.git", Icon: IconGit },
  { tab: "more", label: "tab.more", Icon: IconMore },
];

type BadgeKind = { kind: "count"; n: number; tone: "ai" | "plain" } | { kind: "dot"; tone: "amber" | "blue" } | null;

function badgeFor(tab: Tab): BadgeKind {
  const s = status.value;
  switch (tab) {
    case "chat":
      return s.cardsWaiting > 0 ? { kind: "count", n: s.cardsWaiting, tone: "ai" } : null;
    case "term":
      return s.termBusy > 0 ? { kind: "dot", tone: "amber" } : null;
    case "git":
      return s.dirty > 0 ? { kind: "count", n: s.dirty, tone: "plain" } : null;
    case "more":
      return s.runsNeedYou > 0 ? { kind: "dot", tone: "blue" } : null;
    default:
      return null;
  }
}

function Badge({ b }: { b: BadgeKind }) {
  if (!b) return <span class="badge-slot" aria-hidden="true" />;
  if (b.kind === "dot") return <span class={`badge-slot`}><span class={`badge-dot tone-${b.tone}`} /></span>;
  return (
    <span class="badge-slot">
      <span class={`badge tone-${b.tone}`}>{b.n > 99 ? "99+" : b.n}</span>
    </span>
  );
}

/** onTabTap opens a tab; tapping the active tab scrolls its view to now. */
export function onTabTap(tab: Tab): void {
  if (route.value.tab === tab && !route.value.rest && !route.value.card) {
    const el = document.querySelector<HTMLElement>(`[data-view="${tab}"] .view-body`);
    if (el) el.scrollTo({ top: tab === "chat" || tab === "term" ? el.scrollHeight : 0, behavior: "smooth" });
    return;
  }
  goTab(tab);
}

export function Nav({ variant, active }: { variant: "tabs" | "rail"; active: Tab }) {
  return (
    <nav class={variant === "tabs" ? "tabbar" : "rail"} aria-label={t("tabs.label")}>
      {NAV_ITEMS.map(({ tab, label, Icon }) => {
        const b = badgeFor(tab);
        const extra = b && b.kind === "count" ? ` (${b.n})` : "";
        return (
          <button
            key={tab}
            type="button"
            class={`nav-item${active === tab ? " is-active" : ""}`}
            aria-current={active === tab ? "page" : undefined}
            aria-label={t(label) + extra}
            onClick={() => onTabTap(tab)}
          >
            <span class="nav-icon">
              <Icon />
              <Badge b={b} />
            </span>
            <span class="nav-label">{t(label)}</span>
          </button>
        );
      })}
    </nav>
  );
}
