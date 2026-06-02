# Security

## Local Execution Model

Weft is a local terminal dashboard and process supervisor. It runs agent tasks, shell tasks, configured commands, and title hooks as local processes with the same permissions as the user running Weft. It is not a sandbox.

Any task launched through Weft can read and write files, access environment variables, use local credentials, and make network requests to the same extent it could if run directly in your terminal. Treat task commands and agent prompts as trusted local execution. Be careful with secrets, production credentials, destructive commands, and untrusted repositories.

The Weft runtime uses a local Unix socket for client/supervisor IPC. The current Weft binary does not implement telemetry or remote reporting. Configured tasks, supported agents, and title hooks may still make network requests as normal local processes. The optional `hooks/auto-title-openai.sh` helper calls the OpenAI API only if configured as `title_hook_command` and given credentials.

## Stored Local Data

Installed releases store runtime files under `~/.weft/`; source and worktree runs use a checkout-local `.weft/` directory when possible, unless `WEFT_HOME` or `WEFT_ROOT` is set.

`state.json` stores workspace, group, and task metadata, including task type ids, titles, statuses, generated title metadata, supported-agent metadata, selected/active ids, and terminal cwd metadata. It does not store live PTY handles, process ids, or normal live task output.

Task output is kept in supervisor memory for the live console and preview. It can be written to disk in narrower cases: temporary `terminal-upgrade-snapshots/` files for idle shell task upgrade restart, agent-owned local storage, shell history, and files produced by the commands you run. Supervisor/client logs are for Weft runtime output, not normal task transcripts; backups can copy those logs along with state and config. Title hook stderr can be saved in `state.json` as an auto-title error.

## Supported Versions

Security fixes target the latest published Weft release.

## Reporting a Vulnerability

Use GitHub private vulnerability reporting when available. Include the affected version, the smallest reproduction you can share safely, and the expected impact.

If private vulnerability reporting is unavailable, open a minimal GitHub Issue asking for maintainer contact and do not include exploit details, secrets, private task output, or sensitive local paths.
