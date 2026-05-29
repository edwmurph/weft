---
name: codux-refactor
description: Use when Codex is asked to refactor the codux repo, minimize or simplify code, reduce duplication, improve maintainability, update repo docs or AGENTS.md instructions, identify workflow/process inefficiencies, evaluate alternative libraries for possible integration, or maintain the repo-local refactor suggestion log.
---

# Codux Refactor

## Purpose

Refactor codux with a bias toward less code, clearer boundaries, accurate docs, and repeatable maintenance habits. Preserve the tmux-native CLI behavior unless the user explicitly asks for a behavior change.

## Workflow

1. Read the applicable `AGENTS.md` files, `README.md`, `go.mod`, relevant Go code/tests, and `references/suggestion-log.md`.
2. For broad refactor asks, identify 1-3 concrete targets first: duplicated logic, dead or over-complex code, brittle boundaries, stale docs, weak tests, or slow/manual process steps.
3. Prefer the smallest simplification that preserves behavior. Remove code before adding abstractions; add an abstraction only when it reduces real complexity.
4. Update docs, examples, and AGENTS.md files in the same change when implementation behavior or workflow changes make them stale.
5. Run the repo workflow after edits: `gofmt -w cmd internal tests`, `go test ./...`, `CODUX_RUN_INTEGRATION=1 go test ./...`, and `go build ./cmd/codux`.
6. Before finishing, update `references/suggestion-log.md` with the suggestions made, outcome, evidence, and any deferred follow-up.

## Refactor Heuristics

- Minimize code paths around tmux, PTY, rendering, state, and navigation rather than spreading behavior across modules.
- Keep tmux and child-process boundaries easy to mock; avoid tests that require a live tmux server unless explicitly needed.
- Prefer structured parsing and existing helpers over ad hoc string handling when the boundary is already structured.
- Treat config/state changes as user-facing: update README examples and tests together.
- Keep dependency additions rare; if a dependency replaces local code, verify it reduces net complexity and is maintained.

## Process Improvements

- Look for repeated manual steps that could become a command, script, test fixture, or clearer AGENTS.md instruction.
- If a repo instruction or this skill is inefficient, finish the requested work and note the concrete improvement.
- Keep workflow changes practical: fewer commands, clearer validation, less hidden state, or safer defaults.

## Library Research

- Use current primary sources when evaluating alternatives, especially for tmux, terminal rendering, CLI, config, and test tooling.
- Compare fit by API surface, maintenance activity, dependency weight, license, Go version support, and how much codux code it can delete.
- Recommend integration only when the library materially improves correctness or simplicity; otherwise log the idea as deferred or rejected.

## Suggestion Log

Use `references/suggestion-log.md` as durable memory for refactor ideas. Log only concrete suggestions that were made to the user or implemented, and keep entries short enough to scan.
