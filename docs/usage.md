# Usage

Weft has one dashboard for agent sessions and shell commands across projects. Each project directory is a workspace. Inside a workspace, tasks can be grouped however you like.

## Dashboard Basics

- `Workspaces` shows project directories.
- `Tasks` shows tasks for the selected workspace.
- `Task Live Preview` shows read-only output for the selected task when there is room.
- `Task Console` opens a task for direct input.

Common keys:

```text
n       Create a task
g       Create a group
w       Add a workspace
e       Edit the selected workspace, group, or task title
m       Move the selected task
Enter   Open the selected task, group, or form action
C-b     Return from a task to the dashboard
?       Show shortcuts
C-c     Quit from dashboard focus
```

Create an agent task from the CLI:

```sh
weft new --type codex "Review the failing integration test"
```

Create a shell task from the CLI:

```sh
weft new --type shell "Project shell"
```

The default shell task starts an interactive login shell. Open it with `Enter`, run commands normally, then return to the dashboard with `C-b`.

Persist a handy link from inside a Weft-managed Codex task:

```sh
weft task notes set "Waiting on CI: https://github.com/example/repo/actions/runs/123"
```

Persist longer notes for Task Tools:

```sh
printf '%s\n' "CI is still running." "Check the release notes before shipping." | weft task notes detail set
```

Use `weft task notes show` to print the short note, `weft task notes detail show` to print the longer notes, and the matching `clear` commands when either is stale. The focused Codex `Task Console` shows the short note in its top border; `C-]` opens Task Tools with notes and console commands.

## Common Commands

```text
weft                         Open the dashboard.
weft --clear                 Clear runtime state, then open a fresh dashboard.
weft --no-attach             Start or reuse the background runtime without opening the dashboard.
weft refresh                 Request a fresh dashboard snapshot.
weft status [--json]         Show workspace, group, and task state.
weft version                 Show CLI, runtime, and dashboard versions.
weft doctor                  Check local runtime and task command health.
weft doctor attention        Check terminal notification settings.
weft doctor keys             Diagnose terminal key encoding.
weft doctor memory           Diagnose supervisor and task memory use.

Tasks and organization:
weft new [--type id] [title] Create a task.
weft task notes set <text> Set a short note for the current Codex task.
weft task notes detail set Set longer notes for the current Codex task.
weft task notes show       Show the short note for the current Codex task.
weft task notes clear      Clear the short note for the current Codex task.
weft select <id>             Make a task active.
weft rename [id] <title>     Rename the selected task or the given task.
weft close [id]              Close the active client or a task.
weft group add <name>        Add a group in the current workspace.
weft workspace add <path>    Add a workspace to the dashboard.
weft move-left               Move the selected task out of its group.
weft move-right              Move the selected task into the selected group.

Runtime and configuration:
weft close --kill [--yes]    Stop the runtime and all tasks.
weft clear                   Prompt, then delete Weft runtime state.
weft backup create           Back up config, state, and logs.
weft backup list             List saved runtime backups.
weft backup restore <id>     Restore config and state from a backup.
weft config info             Show runtime paths and active config.
weft config show             Print config.toml.
weft config init [--force]   Write the default config.
weft skill install           Install the bundled Codex skill.
```

Run `weft close` without an id to detach the active Weft client while tasks keep running. Pass an id to close a task. Use `weft close --kill` only when you want to stop the runtime and all tasks after confirmation.

When the interactive dashboard is open, Weft sets the terminal tab title to `Weft`.

## Upgrades

After `brew upgrade weft`, reopen the dashboard with `weft`.

If only the client needed to reopen, Weft is current. If an older background runtime is still running, the dashboard shows an upgrade banner. When running tasks are safe to resume or restart, Weft shows `U` as the upgrade action. If tasks are still busy, `U` can schedule auto-upgrade from the open dashboard; keep that dashboard open and Weft will run the same safe upgrade once every Codex task is idle/resumable and every shell task is idle.

Codex agent tasks can be resumed after a confirmed dashboard upgrade when Weft has a saved resume id. Terminal tasks can restart only when idle, retaining prior scrollback as read-only history and launching from the latest OSC 7 cwd. This is not shell resume: jobs, environment mutations, shell variables, and unsubmitted input are not preserved, so finish important command work before upgrading.

## Key Diagnostics

Use `weft doctor keys` when Option/Alt shortcuts do not behave like the rest of your terminal. It checks Backspace, Option+Backspace, and Ctrl+Backspace, then prints the terminal setting needed when Option is being sent as plain Backspace.

On iTerm2, Weft can offer to update the current/default profile after writing a plist backup.

## Attention Diagnostics

Use `weft doctor attention` when terminal attention is enabled but iTerm2 does not show notification popups. On iTerm2, Weft can inspect the current/default profile, offer to enable Notification Center alerts after writing a plist backup, send a test notification, and print the remaining macOS and iTerm2 filter checks.

## Memory Diagnostics

Use `weft doctor memory` when Weft or its task processes may be using unexpected memory. It reports the current supervisor RSS, descendant task-process RSS, total Weft supervisor RSS on the machine, and warns about other Weft supervisors outside the current runtime so stale runtimes or interrupted tests are easier to spot. It only reports diagnostics; it does not stop or delete processes.
