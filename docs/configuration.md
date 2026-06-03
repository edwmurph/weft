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

During dashboard upgrade, any idle terminal task can restart as a fresh task with saved history/cwd. A terminal task running foreground work blocks `U` until it becomes idle. This is not shell resume: Weft keeps prior visible history as read-only scrollback and starts the fresh command from the latest cwd reported by OSC 7, but jobs, environment mutations, shell variables, and unsubmitted input are not preserved.

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

PTY task commands run from the task workspace through the user's shell with `-lc`. They inherit the parent environment, with `WEFT_ROOT`, `WEFT_HOME`, `WEFT_WORKSPACE`, and `NO_COLOR` removed and `SHELL` normalized to the resolved shell. This means task commands can use the same credentials and environment variables they would see from a normal terminal, unless your shell startup files change them.

Title hooks also run from the task workspace and inherit the environment with `SHELL` normalized. The checked-in `hooks/auto-title-openai.sh` helper reads `OPENAI_API_KEY` from the environment or a local ignored `.env` file and calls the OpenAI API only when you configure it as `title_hook_command`.

## Runtime Paths

Installed releases use:

```text
~/.weft/config.toml
~/.weft/state.json
~/.weft/state.json.lock
~/.weft/weft.sock
~/.weft/weftd.pid
~/.weft/weftd.lock
~/.weft/weftd.log
~/.weft/weft-client.log
~/.weft/backups/
~/.weft/terminal-upgrade-snapshots/
```

Development runs from a source checkout use the checkout-local `.weft/` directory unless `WEFT_HOME`, `WEFT_ROOT`, or `WEFT_ALLOW_MAIN_RUNTIME=1` says otherwise.

`state.json` stores workspace, group, and task metadata, including task type ids, titles, statuses, generated title metadata, provider-neutral live title/status and resume metadata, selected/active ids, and terminal cwd metadata. Normal task terminal output is kept in supervisor memory for the live console and preview, not in `state.json`. Idle shell task upgrade restart can temporarily write visible scrollback and cwd metadata under `terminal-upgrade-snapshots/`; Weft removes those files after restoring them into the restarted task.
