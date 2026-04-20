<!--
PR title must follow Conventional Commits (enforced by the "Validate PR title" check):
  <type>(optional-scope): <subject>
Types: feat, fix, chore, docs, refactor, test, ci, build, perf, style, revert
Use `feat!:` or add `BREAKING CHANGE:` in the body for a breaking change.
The title drives the release-please changelog when this PR is squash-merged.
-->

## Summary

<!-- What changed and why, in 1-3 bullets. -->

## Test plan

<!-- Checklist of how you verified the change. -->

- [ ] `task lint`
- [ ] `task test`
- [ ] `task build`
- [ ] Smoke tests (`go test -tags smoke -v -timeout 120s ./scripts/`) — if touching HTTP client, handlers, or converter
