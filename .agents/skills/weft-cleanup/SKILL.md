---
name: weft-cleanup
description: Broad local cleanup for the weft repo. Use when the user says they are done with Weft worktrees or asks to clean stale Weft local environment resources, including registered .worktrees checkouts, their .weft-runtime/.weft supervisors, stale Git worktree metadata, temp integration-test Weft supervisors, and temp runtime dirs.
---

# Weft Cleanup

Use this skill for broad, deliberate local cleanup after the user says the active Weft worktrees and temp runtimes are disposable.

## Safety Rules

- Start with dry runs. Do not execute destructive cleanup until the user has seen the plan and explicitly confirms.
- Preserve the primary checkout on `main`.
- Preserve the installed-user runtime at `~/.weft` unless the user explicitly asks to stop the live installed Weft supervisor.
- Preserve any worktree the user has not declared disposable.
- Treat `scripts/cleanup-worktrees.sh` as authoritative for registered auxiliary worktrees under `./.worktrees/`.
- Treat `.agents/skills/weft-cleanup/scripts/cleanup-temp-supervisors.sh` as authoritative for stale temp-runtime Weft supervisors not registered as worktrees.
- Do not manually run broad `rm`, `kill`, `pkill`, or `killall` commands. Use the repo scripts so PID/runtime validation stays consistent.

## Dry Run

From the primary Weft checkout, run:

```bash
scripts/cleanup-worktrees.sh --dry-run
bash .agents/skills/weft-cleanup/scripts/cleanup-temp-supervisors.sh --dry-run
go -C . run ./cmd/weft doctor memory || weft doctor memory
git worktree list --porcelain
```

Report:

- registered `.worktrees/` checkouts targeted by `scripts/cleanup-worktrees.sh`
- running worktree supervisors it will stop
- stale temp-runtime supervisors targeted by `cleanup-temp-supervisors.sh`
- runtime dirs that would be removed only with `--remove-runtime-dirs`
- supervisors/runtimes kept, especially `~/.weft`
- any dirty or unexpected worktrees that make broad cleanup unsafe

## Execute After Confirmation

Only after explicit confirmation that broad cleanup is safe:

```bash
scripts/cleanup-worktrees.sh --yes
bash .agents/skills/weft-cleanup/scripts/cleanup-temp-supervisors.sh --yes --remove-runtime-dirs
```

Use `--remove-runtime-dirs` only for broad cleanup after confirmation. Without it, the temp-supervisor script stops matching supervisors but leaves their temp runtime directories on disk.

## Post Checks

Run:

```bash
scripts/cleanup-worktrees.sh --dry-run
bash .agents/skills/weft-cleanup/scripts/cleanup-temp-supervisors.sh --dry-run
git worktree list --porcelain
git branch --format="%(refname:short)"
go -C . run ./cmd/weft doctor memory || weft doctor memory
```

Report what remains, including any kept installed supervisor, any kept external worktrees, and any temp supervisors that could not be safely classified.

## Local Branches

If broad cleanup leaves obsolete local branches, list merged branch candidates with:

```bash
git branch --merged main --format="%(refname:short)"
```

Do not delete branches unless the user explicitly confirms that branch cleanup is also in scope.
