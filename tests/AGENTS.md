# Agent Instructions (tests)

## Testing Conventions

- Use Go tests; keep deterministic unit coverage in `internal/...`.
- Avoid touching real user state in tests. Use temporary `WEFT_HOME` and `WEFT_WORKDIR`.
- Live tests under `tests/integration/` must isolate Weft with temporary `WEFT_HOME`, temporary `WEFT_WORKDIR`, and fake `codex_command`.
- Prefer one primary-flow live test over many narrow supervisor/client tests when the setup path repeats.
