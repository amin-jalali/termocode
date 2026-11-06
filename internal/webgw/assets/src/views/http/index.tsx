// The HTTP client of the desktop workbench (WEBP-HTTP): its side bar and
// the panel's HTTP RESPONSE tab, registered here (lib/views.ts).
import { h } from "preact";
import { registerPanel, registerSide, TERMINAL_ORDER } from "../../lib/views";
import { HTTPPanel, HTTPView } from "./HTTPView";

registerSide("http", () => <HTTPView />);
registerPanel({ id: "http-response", label: "http.response", order: TERMINAL_ORDER + 20, view: () => <HTTPPanel /> });
