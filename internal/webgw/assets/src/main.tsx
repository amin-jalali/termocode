// Entry of the termocode web UI. Built by cmd/webbuild (esbuild Go API).
import { h, render } from "preact";
import "./styles/app.css";

const root = document.getElementById("app");
if (root) render(<p>termocode</p>, root);
