---
name: remove-ai-slop
description: Clean verbose prose and unnecessary generated code while preserving behavior and local style.
---

Inspect the requested diff and nearby code before changing it.
Remove comments that restate code, redundant wrappers, needless abstractions, and defensive branches that cannot occur under the actual contract.
Keep checks at trust boundaries, useful domain explanations, and established project patterns.
In prose, replace vague claims and repeated summaries with concrete facts; preserve technical meaning.
Keep the cleanup scoped to the request. Verify behavior with the existing focused checks and describe any remaining uncertainty.
