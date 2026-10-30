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
