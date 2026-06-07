# Configuration

Weft stores its normal installed-user config at:

```text
~/.weft/config.toml
```

Create or reset the file with:

```sh
weft config init
```

## Task Types

Task types define what `n` can create in the dashboard. A task type can be an agent or a configured shell command.

The default config includes a Codex agent task and a Shell command task:

```toml
default_task_type = "codex"
title_hook_command = ""
title_hook_timeout_seconds = 10

[terminal_attention]
enabled = false
request_attention = "once"

[task_context]
enabled = true

[task_types.codex]
label = "Codex"
kind = "codex"
command = "codex"
badge = "[codex]"
title_template = "{live}"

[task_types.shell]
label = "Shell"
kind = "terminal"
command = "exec \"$SHELL\" -l"
badge = "[shell]"
title_template = "Shell"
```

`codex` is the only supported agent kind today. Additional agents can be added upon request.

Configured command tasks use `kind = "terminal"`. You can add more configured commands, for example:

```toml
[task_types.tests]
label = "Tests"
kind = "terminal"
command = "go test ./..."
badge = "[test]"
title_template = "Tests"
```

During dashboard upgrade, any idle terminal task can restart as a fresh task with saved history/cwd. A terminal task running foreground work blocks immediate upgrade until it becomes idle; pressing `U` while blocked can schedule auto-upgrade from the open dashboard. This is not shell resume: Weft keeps prior visible history as read-only scrollback and starts the fresh command from the latest cwd reported by OSC 7, but jobs, environment mutations, shell variables, and unsubmitted input are not preserved.

## Terminal Attention

Enable terminal attention when you want Weft to signal attention outside the dashboard:

```toml
[terminal_attention]
enabled = true
request_attention = "once"
```

Today the attention provider is iTerm2-only. When the attached client is running in iTerm2, Weft posts an iTerm2 notification when an existing task newly needs attention after the initial snapshot. Notification text uses the task title, for example `Tests needs attention`, and avoids shell/session title prefixes when the task has its own title. Creating a new task does not notify, and the active task console is treated as already acknowledged. `request_attention = "once"` also asks iTerm2 to draw attention once for that transition. This includes configured shell commands that finish foreground work, such as a `sleep` command returning to the shell prompt, even when other tasks already need attention. Depending on iTerm2 and macOS settings, the notification can appear as a popup and the attention request can show iTerm2's tab attention indicator or request Dock attention. Weft does not set iTerm2's session badge text because that renders as an in-terminal watermark.

Run `weft doctor attention` from iTerm2 if the Dock bounces but no notification popup appears. It can inspect and offer to enable iTerm2 profile notifications, then sends a test notification and prints the remaining iTerm2 Filter Alerts and macOS Notification settings to check.

## Task Notes

Codex tasks can persist notes for their own task. `weft task notes preview set` stores a compact one-line shortform note for `Task Live Preview`. `weft task notes set` stores a concise one-line medform note that appears in the focused Codex `Task Console` heading and acts as the preview fallback. `weft task notes detail set` stores longer multi-line longform notes that appear in Task Tools, opened with `C-]`.

Task context is enabled by default. Disable it with:

```toml
[task_context]
enabled = false
```

## Title Templates

Task rows render a stored title template. New tasks copy the template from their task type unless you provide a title.

Supported variables:

```text
{title}      user-configured task title
{auto}       generated title from the first submitted message or command
{live}      live task title
{status}     live task status, falling back to task lifecycle status
{workspace}  display workspace path
{group}      flat group name
```

The retired `{codex}` variable is unsupported; use `{live}` for the live task title.

Examples:

```text
{title}
{live}
{auto}
{status} {auto}
{group}: {title}
```

## Title Hooks

Set `title_hook_command` to generate `{auto}` titles:

```toml
title_hook_command = "/path/to/weft/hooks/auto-title-openai.sh"
title_hook_timeout_seconds = 10
```

When configured, Weft runs the hook from the task workspace and sends JSON on stdin. The payload includes the first message plus `title_columns` and `auto_title_columns` hints that account for the current Tasks pane width, task marker, widest configured task-type badge, and title-template fields such as `{status}`. The first non-empty stdout line becomes the generated title.

Codex agent tasks use the first submitted chat message. Configured command tasks opt in by including `{auto}` in `title_template`, then use the first typed command.

The checked-in `hooks/auto-title-openai.sh` script is one example hook. It reads `OPENAI_API_KEY` from the environment or a local ignored `.env` file and calls the OpenAI Responses API. Its prompt tells the model to stay within `auto_title_columns` so generated titles fit task rows without ellipsis truncation. The script retries transient curl failures twice by default; set `OPENAI_TITLE_CURL_RETRIES` and `OPENAI_TITLE_CURL_RETRY_DELAY_SECONDS` to tune that behavior. You can replace it with any command that follows the same stdin/stdout contract.

## Task Environment

PTY task commands run from the task workspace through the user's shell with `-lc`. They inherit the parent environment, with `WEFT_ROOT`, `WEFT_HOME`, `WEFT_WORKSPACE`, and `NO_COLOR` removed and `SHELL` normalized to the resolved shell. Codex agent tasks then receive task-scoped `WEFT_HOME`, `WEFT_TASK_ID`, `WEFT_TASK_TYPE_ID`, and `WEFT_TASK_KIND=codex` so commands inside the task can address their own Weft metadata. Configured shell tasks do not receive those task-scoped variables. This means task commands can use the same credentials and environment variables they would see from a normal terminal, unless your shell startup files change them.

Title hooks also run from the task workspace and inherit the environment with `SHELL` normalized. The checked-in `hooks/auto-title-openai.sh` helper reads `OPENAI_API_KEY` from the environment or a local ignored `.env` file and calls the OpenAI API only when you configure it as `title_hook_command`.

## Runtime Paths

Installed releases use:

```text
~/.weft/config.toml
~/.weft/state.json
~/.weft/state.json.lock
~/.weft/task-context.json
~/.weft/task-context.json.lock
~/.weft/weft.sock
~/.weft/weftd.pid
~/.weft/weftd.lock
~/.weft/weftd.log
~/.weft/weft-client.log
~/.weft/backups/
~/.weft/terminal-upgrade-snapshots/
```

Development runs from a source checkout use the checkout-local `.weft-runtime/` directory unless `WEFT_HOME`, `WEFT_ROOT`, or `WEFT_ALLOW_MAIN_RUNTIME=1` says otherwise.

`state.json` stores workspace, group, and task metadata, including task type ids, titles, statuses, generated title metadata, provider-neutral live title/status and resume metadata, selected/active ids, and terminal cwd metadata. `task-context.json` stores Codex task notes separately from state. Normal task terminal output is kept in supervisor memory for the live console and preview, not in `state.json`. Idle shell task upgrade restart can temporarily write visible scrollback and cwd metadata under `terminal-upgrade-snapshots/`; Weft removes those files after restoring them into the restarted task.
