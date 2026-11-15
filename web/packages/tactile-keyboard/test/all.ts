// The package's tests in one bundle; the Go test (internal/webgw/assets,
// TestTactileKeyboardPackage) bundles this file with esbuild and runs it
// with node.
import "./layout.test";
import "./engine.test";
import "./dom.test";
import "./dock.test";
import "./text.test";
import { run } from "./t";

void run();
