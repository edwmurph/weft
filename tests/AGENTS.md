# Agent Instructions (tests)

## Testing Conventions

- Use pytest; keep tests deterministic and fast.
- Prefer `tmp_path` over touching real user state (no `~/.codux/**` writes in tests).
- Avoid depending on a real tmux server; mock `codux/tmux.py` boundaries where possible.
- Tests under `tests/integration/` may use a real tmux server only when each test isolates `CODUX_HOME`, `CODUX_WORKDIR`, tmux session names, and tmux sockets so parallel worktrees cannot collide.
- Prefer `tests/integration/` for primary flows that require real tmux behavior, and consolidate related assertions into those flows instead of adding many narrow live-runtime tests.
- Keep broad edge-case coverage in fast unit tests unless a real tmux/process interaction is the behavior under test.
