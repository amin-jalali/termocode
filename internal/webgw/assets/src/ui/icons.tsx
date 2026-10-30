// Line icons for the tab bar and the strip, drawn as SVG elements (no
// icon font, no markup strings). 24-unit grid, 1.75 stroke, currentColor.
import { h, type ComponentChildren } from "preact";

function Icon({ children, size = 22 }: { children: ComponentChildren; size?: number }) {
  return (
    <svg
      class="icon"
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="1.75"
      stroke-linecap="round"
      stroke-linejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      {children}
    </svg>
  );
}

export const IconChat = () => (
  <Icon>
    <path d="M4 5.5h16v10H9.5L5 19.5v-4H4z" />
    <path d="M12 8.2l.8 1.9 1.9.8-1.9.8-.8 1.9-.8-1.9-1.9-.8 1.9-.8z" fill="currentColor" stroke="none" />
  </Icon>
);

export const IconFiles = () => (
  <Icon>
    <path d="M3.5 6.5a1.5 1.5 0 0 1 1.5-1.5h4l2 2h8a1.5 1.5 0 0 1 1.5 1.5v9a1.5 1.5 0 0 1-1.5 1.5H5a1.5 1.5 0 0 1-1.5-1.5z" />
  </Icon>
);

export const IconTerm = () => (
  <Icon>
    <rect x="3" y="4.5" width="18" height="15" rx="2" />
    <path d="M7 10l3 2.5L7 15M12.5 15.5H17" />
  </Icon>
);

export const IconGit = () => (
  <Icon>
    <circle cx="6.5" cy="5.5" r="2" />
    <circle cx="6.5" cy="18.5" r="2" />
    <circle cx="17.5" cy="8.5" r="2" />
    <path d="M6.5 7.5v9M17.5 10.5c0 4-5 3.5-9.5 6.5" />
  </Icon>
);

export const IconMore = () => (
  <Icon>
    <circle cx="5.5" cy="12" r="1.25" fill="currentColor" />
    <circle cx="12" cy="12" r="1.25" fill="currentColor" />
    <circle cx="18.5" cy="12" r="1.25" fill="currentColor" />
  </Icon>
);

export const IconBranch = () => (
  <Icon size={14}>
    <circle cx="7" cy="5.5" r="2" />
    <circle cx="7" cy="18.5" r="2" />
    <circle cx="17" cy="8" r="2" />
    <path d="M7 7.5v9M17 10c0 4-4.5 3.5-8.5 6.5" />
  </Icon>
);

export const IconMoon = () => (
  <Icon size={16}>
    <path d="M19 14.5A7.5 7.5 0 0 1 9.5 5a7.5 7.5 0 1 0 9.5 9.5z" />
  </Icon>
);

export const IconSun = () => (
  <Icon size={16}>
    <circle cx="12" cy="12" r="3.5" />
    <path d="M12 3v2M12 19v2M3 12h2M19 12h2M5.6 5.6l1.4 1.4M17 17l1.4 1.4M5.6 18.4L7 17M17 7l1.4-1.4" />
  </Icon>
);

// Small icons of the desktop workbench and its toolbars (16 px).
export const IconClose = () => (
  <Icon size={14}>
    <path d="M7 7l10 10M17 7L7 17" />
  </Icon>
);

export const IconChevronDown = () => (
  <Icon size={16}>
    <path d="M6.5 9.5l5.5 5.5 5.5-5.5" />
  </Icon>
);

export const IconFileSmall = () => (
  <Icon size={14}>
    <path d="M6 3.5h8l4 4v13H6z M14 3.5v4h4" />
  </Icon>
);

export const IconSidebar = () => (
  <Icon size={16}>
    <rect x="3.5" y="4.5" width="17" height="15" rx="2" />
    <path d="M9.5 4.5v15" />
  </Icon>
);

export const IconPanel = () => (
  <Icon size={16}>
    <rect x="3.5" y="4.5" width="17" height="15" rx="2" />
    <path d="M3.5 14.5h17" />
  </Icon>
);

export const IconChatSide = () => (
  <Icon size={16}>
    <rect x="3.5" y="4.5" width="17" height="15" rx="2" />
    <path d="M14.5 4.5v15" />
  </Icon>
);

export const IconWrap = () => (
  <Icon size={16}>
    <path d="M4 6.5h16M4 12h13a3 3 0 0 1 0 6h-4M4 17.5h5M14.5 15.5L12.5 18l2 2.5" />
  </Icon>
);

export const IconUndo = () => (
  <Icon size={16}>
    <path d="M9 7.5L5 11.5l4 4M5.5 11.5H15a4.5 4.5 0 0 1 0 9h-2" />
  </Icon>
);

export const IconRedo = () => (
  <Icon size={16}>
    <path d="M15 7.5l4 4-4 4M18.5 11.5H9a4.5 4.5 0 0 0 0 9h2" />
  </Icon>
);

export const IconPencil = () => (
  <Icon size={16}>
    <path d="M4.5 19.5l1-4L16 5a2.1 2.1 0 0 1 3 3L8.5 18.5z M14 7l3 3" />
  </Icon>
);

export const IconLaptop = () => (
  <Icon size={16}>
    <rect x="5" y="5.5" width="14" height="9.5" rx="1.5" />
    <path d="M3 18.5h18" />
  </Icon>
);

export const IconSave = () => (
  <Icon size={16}>
    <path d="M5 4.5h11l3.5 3.5v11.5H5z M8 4.5v5h7v-5 M8 19.5v-5.5h8v5.5" />
  </Icon>
);

export const IconRows = () => (
  <Icon size={16}>
    <path d="M5 7h14M5 12h14M5 17h9" />
  </Icon>
);

/** Cod is one icon of the codicon font (styles/codicons.css), 16 px or
 * 24 px (the activity bar). */
export function Cod({ name, big }: { name: string; big?: boolean }) {
  return <span class={`cod cod-${name}${big ? " cod-24" : ""}`} aria-hidden="true" />;
}
