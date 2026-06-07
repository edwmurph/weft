---
name: weft
description: Use when running inside a Weft task, updating Weft task notes, or diagnosing local Weft dashboard, supervisor, runtime, config, state, version, protocol, or task-type issues.
---

# Weft

Use this skill when running as Codex inside a Weft task or when diagnosing a local Weft dashboard issue.

## Task Notes

- If `WEFT_TASK_ID` is set, the current process is running inside a Weft task.
- If `WEFT_TASK_ID` is not set but you know the current Codex session id, run `weft status --json` and find the Codex task whose `resume_id` matches that session id. With `jq`: `weft status --json | jq -r --arg sid "$CODEX_SESSION_ID" '.tasks[] | select(.resume_id == $sid) | .id'`.
- After you know the task id, pass `--task <id>` to update that task's notes from any shell connected to the same Weft runtime.
- Keep three versions available for each useful task update: shortform, medform, and longform.
- Use `weft task notes preview set "<shortform>"` to persist a compact one-line shortform for Task Live Preview.
- Use `weft task notes set "<medform>"` to persist a concise one-line medform for the focused Codex Task Console heading. Task Live Preview falls back to this medform when no preview shortform is set.
- Use `weft task notes detail set "<longform>"` or piped input such as `printf '%s\n' "$note" | weft task notes detail set` for the longer Task Tools overlay note.
- Use `weft task notes preview show`, `weft task notes show`, and `weft task notes detail show` to read the three note forms.
- Use `weft task notes preview clear`, `weft task notes clear`, or `weft task notes detail clear` when the matching note is stale or no longer useful.
- Opening Task Tools with `C-]` shows Task Notes and Console Commands as separate sections.
- For another Codex task, pass `--task <id>` to `set`, `show`, or `clear` in any notes command family.
- Follow any user, repo, or installed-skill instructions that define how task notes should be used.
- If nothing else says otherwise, use task notes for handy links to running or slow external work: GitHub Actions workflow runs, pull request reviews, issue threads, deployment logs, long-running job dashboards, or any URL the user may need to reopen later.
- Keep preview shortforms very short, heading medforms concise and durable, and longforms detailed enough for handoff. Prefer the best current link plus status or next check. Avoid secrets.

## Weft Diagnostics

- Start with `weft status` to see supervisor, workspace, group, and task state.
- Use `weft version` to compare CLI, supervisor, dashboard, and protocol versions.
- Use `weft config info` for runtime paths and active task type IDs.
- Use `weft config show` to inspect task type commands, title hooks, and terminal attention settings.
- Inspect logs from the runtime directory shown by `weft config info`, especially `weftd.log` and `weft-client.log`.

## Configuration Suggestions

- Task types live under `[task_types.<id>]`; use `kind = "codex"` only for the built-in Codex type and `kind = "terminal"` for shell commands.
- Title hooks use `title_hook_command` and should output one concise title line.
- Terminal attention is configured under `[terminal_attention]`; use `enabled = true` and choose `request_attention = "once"` or `"off"`.

## Safety

- Do not run destructive commands such as `weft clear` or `weft close --kill` without explicit user confirmation.
- Before creating a GitHub issue, prepare a sanitized summary with versions, config snippets, logs, and reproduction steps. Run `gh issue create` only after the user confirms.
