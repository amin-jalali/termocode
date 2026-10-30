// The Run, Test and Debug views of the desktop workbench (WEBP-4): they
// register their side bars and panel tabs here (lib/views.ts), so the
// workbench's layout stays as it is.
import { h } from "preact";
import { registerPanel, registerSide, TERMINAL_ORDER } from "../../lib/views";
import { TestingView } from "./TestingView";
import { TestResults } from "./TestResults";

registerSide("testing", () => <TestingView />);
registerPanel({ id: "test-results", label: "test.results", order: TERMINAL_ORDER + 10, view: () => <TestResults /> });
