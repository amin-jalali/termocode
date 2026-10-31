---
name: debugging
description: Diagnose a reproducible failure, isolate its cause, and verify a focused fix.
---

Capture the failing input, observed result, expected result, and relevant environment. Start with the smallest reproduction available.
Trace the failure to its cause; use targeted logs or experiments to distinguish plausible explanations. Avoid changing several unrelated things at once.
Add a regression test that fails for the observed bug before fixing it when the test harness supports this.
Fix the cause at the narrowest correct boundary. Check neighboring edge cases and run the focused regression checks.
Remove temporary instrumentation and report the cause, change, evidence, and any remaining limits.
