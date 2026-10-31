---
name: review-work
description: Review a patch for bugs, regressions, and missing tests; report concrete findings.
---

Read the diff and relevant callers, tests, and repository rules. Trace changed inputs through their consumers.
Prioritize incorrect behavior, data loss, permission mistakes, races, and compatibility breaks. Explain the trigger, impact, and file location for each finding.
Separate confirmed bugs from questions. Avoid speculative findings and style-only churn.
Run focused checks when useful. State what was checked, what could not be checked, and when no actionable issue was found.
A review request alone does not authorize editing the patch.
