## Summary

Describe the change and why it is needed.

## User-visible behavior

Describe any changed commands, dashboard behavior, config/state behavior, docs, install behavior, or release behavior. Write "None" if there is no user-visible behavior change.

## Validation

List the exact checks you ran and their results. If a check was not run, explain why.

## Checklist

- [ ] I updated `spec.md` for any behavior, UX, command, config, state, or workflow change, or this PR does not change product behavior.
- [ ] I updated public docs, or this PR does not need docs changes.
- [ ] I ran `gofmt -w cmd internal tests`, or no Go files changed.
- [ ] I ran `go test ./...`.
- [ ] I ran `WEFT_RUN_INTEGRATION=1 go test ./...`, or explained why it could not run.
- [ ] I ran `go build ./cmd/weft`.
