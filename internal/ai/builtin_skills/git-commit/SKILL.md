---
name: git-commit
description: Create small commits from the requested changes, following the repository commit style.
---

Read the repository rules, git status, the diff, and recent commit subjects first.
Separate independent behavior changes into commits that build on their own. Keep each behavior with its relevant tests.
Stage named paths or selected hunks; inspect the staged diff for unrelated work and secrets before committing.
Use the local commit style. Report the commit IDs and checks run.
Do not include unrelated edits, rewrite published history, or push unless the user requested it. A request to review does not authorize a commit.
