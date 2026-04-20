## Summary

<!-- What changed and why, in 1-3 bullets. -->

## Test plan

<!-- Checklist of how you verified the change. -->

- [ ] `task lint`
- [ ] `task test`
- [ ] `task build`
- [ ] Smoke tests (`go test -tags smoke -v -timeout 120s ./scripts/`) — if touching HTTP client, handlers, or converter
