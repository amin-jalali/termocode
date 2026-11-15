import { currentWord, hasTrigger, suggest, suggestLocal, type Suggestion } from "../src/completion";
import { autoPair, expandSnippet, newlineIndent } from "../src/smarts";
import { editorSuggestions, padStepper } from "../src/editing";
import { eq, ok, test } from "./t";

test("G6 currentWord is the [A-Za-z0-9_] run before the caret", () => {
  eq(currentWord("foo.barBaz", 10), "barBaz");
  eq(currentWord("a b_1", 5), "b_1");
  eq(currentWord("x(", 2), "");
  eq(currentWord("abc", 99), "abc", "offset clamps");
});

test("G7 hasTrigger: a word or right after a dot", () => {
  ok(hasTrigger("fo", 2));
  ok(hasTrigger("x.", 2));
  ok(!hasTrigger("x ", 2));
});

test("G8 G12 suggestLocal: buffer words, never the word being typed", () => {
  const text = "process printLine print pr";
  const got = suggestLocal(text, text.length).map((s) => s.label);
  eq(got, ["print", "process", "printLine"]);
  eq(suggestLocal("abc ", 4), [], "no word, no local suggestions");
});

test("G10 ranking: exact > prefix > fuzzy; LSP +300; shorter first; limit 12", async () => {
  const lsp: Suggestion[] = [{ label: "prxLsp", insertText: "prxLsp()", detail: "fn" }];
  const text = "pxr parse prefix prx";
  const got = await suggest(text, text.length, async () => lsp);
  eq(got[0].label, "prxLsp", "LSP prefix 400+300 beats buffer prefix");
  eq(got.map((s) => s.label).slice(1), ["prefix"], "fuzzy buffer word after");
  ok(!got.some((s) => s.label === "parse"), "parse is not a subsequence of prx");
  const many = Array.from({ length: 30 }, (_, i) => `aa${i}`).join(" ") + " aa";
  eq((await suggest(many, many.length)).length, 12);
});

test("G10 fuzzy subsequence matches score below prefix", () => {
  const text = "getValue gv";
  eq(suggestLocal(text, text.length).map((s) => s.label), ["getValue"]);
  const t2 = "gvx getValue gv";
  eq(suggestLocal(t2, t2.length).map((s) => s.label), ["gvx", "getValue"]);
});

test("G9 G11 dot trigger lists buffer identifiers incl. single-char (BH-17)", async () => {
  const text = "var i = 0;\nfoo.";
  const labels = (await suggest(text, text.length)).map((s) => s.label);
  ok(labels.includes("i"), labels.join(","));
  eq(suggestLocal(text, text.length), [], "the bare dot needs suggest()");
});

test("H1 H2 H3 auto pairs and skips", () => {
  for (const [o, c] of [["(", ")"], ["[", "]"], ["{", "}"], ['"', '"'], ["'", "'"], ["`", "`"]]) eq(autoPair(o, ""), { text: o + c, caretBackBy: 1, skip: false }, o);
  eq(autoPair('"', '"'), { text: "", caretBackBy: 1, skip: true });
  eq(autoPair(")", ")"), { text: "", caretBackBy: 1, skip: true });
  eq(autoPair(")", ""), null);
  eq(autoPair("a", ""), null);
});

test("H4 newline indent keeps the indent, +2 after { : ( [", () => {
  eq(newlineIndent("  foo"), "  ");
  eq(newlineIndent("x\n  if (a) {"), "    ");
  eq(newlineIndent("def f():"), "  ");
  eq(newlineIndent("\tcall("), "\t  ");
  eq(newlineIndent("  arr = [ "), "    ");
  eq(newlineIndent(""), "");
});

test("H5 keyword snippets per language, caret at the marker", () => {
  eq(expandSnippet("if", null), { text: "if () {\n  \n}", caretOffset: 4 });
  eq(expandSnippet("for", "python"), { text: "for  in :\n    ", caretOffset: 4 });
  eq(expandSnippet("for", "go"), { text: "for () {\n  \n}", caretOffset: 5 });
  eq(expandSnippet("while", "python"), { text: "while :\n    ", caretOffset: 6 });
  eq(expandSnippet("func", "go"), { text: "func () {\n  \n}", caretOffset: 5 });
  eq(expandSnippet("fn", "js"), { text: "function () {\n  \n}", caretOffset: 9 });
  eq(expandSnippet("def", "python"), { text: "def ():\n    ", caretOffset: 4 });
  eq(expandSnippet("def", "go"), null);
  eq(expandSnippet("class", "python"), { text: "class :\n    ", caretOffset: 6 });
  eq(expandSnippet("class", null), { text: "class  {\n  \n}", caretOffset: 6 });
  eq(expandSnippet("foo", null), null);
});

test("G13 editor suggestions: snippet chip first, then local words", () => {
  const text = "format forest fo";
  const s = editorSuggestions(text, text.length, null);
  eq(s.map((x) => x.label), ["format", "forest"]);
  const t2 = "format for";
  const s2 = editorSuggestions(t2, t2.length, "go");
  eq(s2[0], { label: "for…", insertText: "for () {\n  \n}", detail: "snippet", caretOffset: 5 });
  eq(s2[1].label, "format");
});

test("H9 pad stepper: one step per 9px across, 22px down, remainder kept", () => {
  const steps: string[] = [];
  const step = padStepper((d) => steps.push(d));
  step(20, 0);
  eq(steps, ["right", "right"]);
  step(-3, 0); // 2 left over, then -3 → -1
  step(-8, 0);
  eq(steps.slice(2), ["left"]);
  step(0, 45);
  eq(steps.slice(3), ["down", "down"]);
  step(0, -23);
  eq(steps.slice(5), ["up"]);
});
