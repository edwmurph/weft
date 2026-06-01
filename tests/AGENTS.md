# Agent Instructions (tests)

## Testing Conventions

- Use Go tests; keep deterministic unit coverage in `internal/...`.
- Avoid touching real user state in tests. Use temporary `WEFT_ROOT`, or temporary `WEFT_HOME` and `WEFT_WORKSPACE` when distinct paths are required.
- Live tests under `tests/integration/` must isolate Weft with temporary `WEFT_ROOT`, or temporary `WEFT_HOME` and `WEFT_WORKSPACE`, plus a fake Codex task command.
- Prefer one primary-flow live test over many narrow supervisor/client tests when the setup path repeats.
