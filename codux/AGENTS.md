# Agent Instructions (codux package)

## Code Style

- Python 3.11+; keep code ruff-compatible (see `pyproject.toml`).
- Prefer small, testable functions and pure helpers; avoid adding new dependencies unless necessary.

## Runtime Constraints

- Treat tmux interactions as potentially flaky: keep boundaries narrow and mockable.
- Preserve atomic state writes and locking semantics in `codux/state.py`.
