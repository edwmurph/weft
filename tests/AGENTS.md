# Agent Instructions (tests)

## Testing Conventions

- Use Go tests; keep deterministic unit coverage in `internal/...`.
- Avoid touching real user state in tests. Use temporary `CODUX_HOME` and `CODUX_WORKDIR`.
- Live tmux tests under `tests/integration/` must isolate tmux with a unique socket and fake `codex_command`.
- Prefer one primary-flow live test over many narrow tmux tests when the setup path repeats.
