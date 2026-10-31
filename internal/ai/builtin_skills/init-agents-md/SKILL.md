---
name: init-agents-md
description: Write repository-specific AGENTS.md guidance from observed build, test, and code conventions.
---

Read existing guidance first. Inspect build files, CI, tests, and representative modules to learn the actual commands and boundaries.
Write concise AGENTS.md instructions containing verified setup and test commands, architecture landmarks, and non-obvious constraints.
Use nested files only when a directory needs different guidance. Keep shared rules at the root; do not duplicate them in every directory.
Preserve existing user instructions. Do not invent policies or claim a command passed without running it.
Exclude credentials, personal paths, and transient task status. Report the files changed and any unverified commands.
