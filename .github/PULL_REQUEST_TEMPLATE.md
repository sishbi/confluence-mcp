<!--
PR title must follow Conventional Commits (enforced by the "Validate PR title" check):
  <type>(optional-scope): <subject>
Types: feat, fix, chore, docs, refactor, test, ci, build, perf, style, revert
Use `feat!:` or add `BREAKING CHANGE:` in the body for a breaking change.
The title drives the release-please changelog when this PR is squash-merged.
-->

## Why

<!-- The motivation that isn't obvious from the diff: the symptom, the missing capability, the wrong contract, the stale doc. One short paragraph. -->

## What changed

<!-- What the PR does. Avoid naming specific files or classes unless it matters for the wider context. -->

-

## Test plan

<!-- How you verified the change. -->

- [ ] `task lint`
- [ ] `task test`
- [ ] `task build`
- [ ] Smoke tests (`go test -tags smoke -v -timeout 120s ./scripts/`) — if touching HTTP client, handlers, or converter
