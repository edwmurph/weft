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

Task types define what `n` can create in the dashboard. A task type can be an
agent or a configured shell command.

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
title_template = "{status} {auto}"

[task_types.shell]
label = "Shell"
kind = "terminal"
command = "exec \"$SHELL\" -l"
badge = "[shell]"
title_template = "Shell"
```

`codex` is the only supported agent kind today. Additional agents can be added
upon request.

Configured command tasks use `kind = "terminal"`. You can add more configured
commands, for example:

```toml
[task_types.tests]
label = "Tests"
kind = "terminal"
command = "go test ./..."
badge = "[test]"
title_template = "Tests"
```

## Title Templates

Task rows render a stored title template. New tasks copy the template from their
task type unless you provide a title.

Supported variables:

```text
{title}      user-configured task title
{auto}       generated title from the first submitted message or command
{codex}      live Codex agent title
{status}     live Codex agent status, falling back to task lifecycle status
{workspace}  display workspace path
{group}      flat group name
```

Examples:

```text
{title}
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

When configured, Weft runs the hook from the task workspace and sends JSON on
stdin. The first non-empty stdout line becomes the generated title.

Codex agent tasks use the first submitted chat message. Configured command
tasks opt in by including `{auto}` in `title_template`, then use the first typed
command.

The checked-in `hooks/auto-title-openai.sh` script is one example hook. It reads
`OPENAI_API_KEY` from the environment or a local ignored `.env` file and calls
the OpenAI Responses API. You can replace it with any command that follows the
same stdin/stdout contract.

## Runtime Paths

Installed releases use:

```text
~/.weft/config.toml
~/.weft/state.json
~/.weft/weft.sock
~/.weft/weftd.pid
~/.weft/weftd.log
~/.weft/backups/
```

Development runs from a source checkout use the checkout-local `.weft/`
directory unless `WEFT_HOME`, `WEFT_ROOT`, or `WEFT_ALLOW_MAIN_RUNTIME=1` says
otherwise.
