# Contributing

Weft is a Go project built around a local supervisor, Bubble Tea, Lip Gloss,
Bubbles, integrated agent tasks, configured command tasks, and
supervisor-owned task PTYs.

## Local Setup

```sh
go test ./...
WEFT_HOME=$PWD/.weft WEFT_WORKSPACE=$PWD go run ./cmd/weft doctor
```

Source-built Weft refuses to use the default `~/.weft` runtime unless
`WEFT_HOME` is set or `WEFT_ALLOW_MAIN_RUNTIME=1` is set intentionally. Keep
manual worktree testing isolated under the ignored `.weft/` directory.

## Checks

Run the full local workflow before offering a change for review:

```sh
go test ./...
WEFT_RUN_INTEGRATION=1 go test ./...
go build ./cmd/weft
```

Live integration tests are opt-in because they start real supervisor and PTY
processes. Each test uses temporary `WEFT_HOME`, temporary `WEFT_WORKSPACE`, and
a fake Codex agent task command.

## Homebrew Publishing

Pushes to `main` run the `Publish Homebrew` workflow. The workflow infers the
semantic version bump from the shipped commit, updates `VERSION` and
`internal/version/version.go`, tags `vX.Y.Z`, creates a GitHub release, and
writes `Formula/weft.rb` to `edwmurph/homebrew-tap`.

Weft stays on the `0.x` prerelease line until an explicit stable `1.0`
decision. While the current version is `0.y.z`, inferred or explicit major
bumps publish `0.(y+1).0` instead of `1.0.0`; breaking changes during
pre-1.0 are minor releases in the `0.x` line. Patch and minor bumps stay in
the normal `0.x` semver shape, such as `0.0.1`, `0.2.0`, or `0.5.2`.
GitHub releases for `v0.*` tags are marked as prereleases. Only a manual
workflow dispatch with `bump=major` and `allow_stable_major=true` may cross
from `0.x` to `1.0.0`.

GitHub release notes are generated from the shipped commits in the release
range. Use concise Conventional Commit-style subjects such as `feat: ...`,
`fix: ...`, `docs: ...`, `refactor: ...`, or `chore: ...`; add commit-body
`Release-Notes:` bullets when the release needs clearer user-facing wording
than the subject alone.

The formula builds the Go binary from source and depends on `go` at build time.
Release/Homebrew builds set Weft's build channel to `release`, which allows the
installed command to own the default `~/.weft` runtime.

## Agent Guidance

Codex-agent workflow and maintenance instructions live in `AGENTS.md`.
Product and runtime details live in `spec.md` and `docs/`.
