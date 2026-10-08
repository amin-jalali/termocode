# <PREFIX>-NN — <title>

> Area: <Area> · Unit: `<unit>`

- **Priority:** Critical / High / Medium / Low
- **Precondition:** <state needed before the steps>
- **Test Data:** <concrete inputs — file names, query strings, key presses>
- **Environment:** <OS · terminal · nvim version · rg yes/no>
- **Traces to:** <feature / ADR> · <`internal/...` path(s)>
- **Automated check:** one of —
  - `✅ internal/<pkg>/<file>_test.go: TestName` (covered now), or
  - `🤖 TODO: <unit|integration>` (could be automated, not yet), or
  - `👁 manual (<why it can't be automated>)`

**Scenario:** <one small, specific behaviour>

## Steps
1. …

## Expected
- <measurable outcome — "the sidebar lists 3 results", not "works">

## Actual
_(filled at run time)_

## Result
`⬜ untested`  ·  ✅ pass / ⚠️ partial / ❌ fail / — n/a
