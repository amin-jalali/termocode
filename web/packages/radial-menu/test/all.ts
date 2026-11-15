// The package's tests in one bundle; the Go test (internal/webgw/assets,
// TestRadialMenuPackage) bundles this file with esbuild and runs it with
// node.
import "./geometry.test";
import "./motion.test";
import "./menu.test";
import "./fab.test";
import "./look.test";
import { run } from "./t";

void run();
