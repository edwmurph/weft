# Agent Instructions (tests)

## Testing Conventions

- Use pytest; keep tests deterministic and fast.
- Prefer `tmp_path` over touching real user state (no `~/.codux/**` writes in tests).
- Avoid depending on a real tmux server; mock `codux/tmux.py` boundaries where possible.

