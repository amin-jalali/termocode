// The design's reference state (mobocode final_blue.png): the open menu
// with the design's own rings, the AI category active, the "tests" wedge
// held to a full progress arc. The rings box is 480 px at the page's top
// left corner, as in final_blue.png. A "#light" hash puts it on a light
// page: the menu must look the same there. The look test
// (internal/webgw/assets, TestRadialMenuLooksLikeTheDesign) screenshots
// this page in headless Chrome and compares it with final_blue.png.
import { mountMenu, type Category } from "../src/index";

const it = (id: string, label: string, icon: string, detail: string) => ({ id, label, icon, detail });

/** DESIGN is mobocode-radial-v6.html's CATS, SAVED aside. */
export const DESIGN: Category[] = [
  {
    id: "edit",
    label: "EDIT",
    icon: "open",
    items: [
      it("edit.open", "open", "open", "open a file"),
      it("edit.find", "find", "find", "search across the project"),
      it("edit.diff", "diff", "diff", "show uncommitted changes"),
      it("edit.format", "format", "format", "reformat the current file"),
      it("edit.comment", "comment", "format", "toggle line comments"),
      it("edit.rename", "rename", "open", "rename the symbol"),
    ],
  },
  {
    id: "ai",
    label: "AI",
    icon: "ask",
    items: [
      it("ai.explain", "explain", "ask", "explain the selected code"),
      it("ai.refactor", "refactor", "format", "restructure, same behavior"),
      it("ai.tests", "tests", "check", "generate unit tests"),
      it("ai.fix", "fix", "bolt", "find & fix a bug"),
      it("ai.docs", "docs", "open", "write documentation"),
      it("ai.review", "review", "diff", "review the current diff"),
    ],
  },
  {
    id: "git",
    label: "GIT",
    icon: "branch",
    items: [
      it("git.commit", "commit", "check", "stage & commit changes"),
      it("git.push", "push", "up", "push to the remote"),
      it("git.pull", "pull", "down", "pull from the remote"),
      it("git.diff", "diff", "diff", "show uncommitted changes"),
      it("git.stash", "stash", "shell", "stash working changes"),
      it("git.log", "log", "format", "view commit history"),
    ],
  },
  {
    id: "run",
    label: "RUN",
    icon: "shell",
    items: [
      it("run.build", "build", "shell", "compile the project"),
      it("run.test", "test", "check", "run the test suite"),
      it("run.lint", "lint", "format", "run the linter"),
      it("run.tasks", "tasks", "set", "run a saved task"),
    ],
  },
];

const light = location.hash === "#light";
document.documentElement.style.setProperty("color-scheme", light ? "light" : "dark");
document.body.style.setProperty("background", light ? "#fafaf8" : "#09090d");
const box = document.getElementById("menu")!;
const m = mountMenu(box, {
  categories: DESIGN,
  favorites: ["edit.find", "git.commit", "edit.format"],
  startOpen: true,
  reducedMotion: true,
});
m.switchCat(2); // SAVED, EDIT, AI
// As in final_blue.png: the pointer rests on "refactor" (hot), and the
// "tests" wedge is held to a full arc. Set after the menu's own frames.
const at = (deg: number, r: number): [number, number] => {
  const a = ((deg - 90) * Math.PI) / 180, b = box.getBoundingClientRect(), s = (b.width - 36) / 600;
  return [b.left + 18 + (300 + r * Math.cos(a)) * s, b.top + 18 + (300 + r * Math.sin(a)) * s];
};
requestAnimationFrame(() =>
  requestAnimationFrame(() => {
    const [x, y] = at(60, 170);
    box.querySelector(".rm-stage")!.dispatchEvent(new PointerEvent("pointermove", { clientX: x, clientY: y, pointerType: "mouse", bubbles: true }));
    const prog = box.querySelectorAll(".rm-prog")[2];
    prog?.setAttribute("opacity", "1");
    prog?.setAttribute("stroke-dashoffset", "0");
    document.body.setAttribute("data-ready", "1");
  }),
);
