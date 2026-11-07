// The phone app's base components (MOBUX-1). The phone views import them
// from here; they come with the phone's lazy chunk (MOB-4), never app.js.
// Styles: styles/phone.css.
export { BoardTile } from "./BoardTile";
export { Block } from "./Block";
export { PhoneCard } from "./Card";
export { PhoneChip, PhoneChips } from "./Chip";
export { Composer, type Attached } from "./Composer";
export { Row, Rows } from "./Row";
export { PhoneShell, ProjectSwitcher, Stream, StreamMsg, TopBar, type LinkState, type ShellComposer, type ShellProject } from "./Shell";
export { PhoneSheet, safeFirst, type SheetAction } from "./Sheet";
export { ProjectTasksView, taskStatus } from "./ProjectTasks";
export { StatusMark } from "./Mark";
export { BOARD_ORDER, STATUS, type Status, type StatusLook } from "./status";
export { ASLEEP_AFTER_MS, SHOW_AFTER_MS, barShown, canAnswer, lastSeen, phoneConn, phoneOnline, type ConnInput, type PhoneConn } from "./conn";
export { ConnLine, ErrorLine, PhoneSkeleton, type SkeletonShape } from "./States";
// live.tsx reads the session: the phone chunk imports it as
// "./phone/live" (startPhoneConn, ConnBar, ConnScope); the kit never does.
