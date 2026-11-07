package prreview

// WorkflowPath is where Git: Set Up PR Review Bot writes the workflow.
const WorkflowPath = ".github/workflows/termocode-review.yml"

// Workflow runs `termocode review` on every pull request, and again when
// a member comments "/termocode review" on one. Only people with write
// access (owner, member, collaborator) can start it from a comment. A
// pull request from a fork gets no secrets, so it is not reviewed.
const Workflow = `# termocode PR review bot: a short AI review on every pull request.
# Only high-priority findings (P0/P1) are posted, as one comment that
# later runs update. Comment "/termocode review" on a pull request to run
# it again. Review rules: the sections of AGENTS.md whose heading has
# "review" in it.
# Needs the repository secret ANTHROPIC_API_KEY (or OPENAI_API_KEY).
name: termocode review

on:
  pull_request:
    types: [opened, synchronize, reopened, ready_for_review]
  issue_comment:
    types: [created]

permissions:
  contents: read
  pull-requests: write
  issues: write

concurrency:
  group: termocode-review-${{ github.event.pull_request.number || github.event.issue.number }}
  cancel-in-progress: true

jobs:
  review:
    if: >-
      (github.event_name == 'pull_request' && !github.event.pull_request.draft) ||
      (github.event_name == 'issue_comment' && github.event.issue.pull_request &&
       startsWith(github.event.comment.body, '/termocode review') &&
       contains(fromJSON('["OWNER","MEMBER","COLLABORATOR"]'), github.event.comment.author_association))
    runs-on: ubuntu-latest
    timeout-minutes: 10
    env:
      GH_TOKEN: ${{ github.token }}
      PR: ${{ github.event.pull_request.number || github.event.issue.number }}
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - name: Check out the pull request
        if: github.event_name == 'issue_comment'
        run: gh pr checkout "$PR"
      - name: Install termocode
        run: curl -fsSL https://raw.githubusercontent.com/amin-jalali/termocode/main/scripts/install.sh | sh
      - name: Review
        run: |
          base=$(gh pr view "$PR" --json baseRefName --jq .baseRefName)
          "$HOME/.local/bin/termocode" review --base "origin/$base" --post "$PR"
        env:
          ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
          OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
`
