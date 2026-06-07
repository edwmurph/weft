<h1 align="center"><img src="assets/weft-logo.svg" alt="Weft" width="360"></h1>

<p align="center">
  <strong>A local terminal dashboard for agents and shell commands.</strong>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/go-1.24.2%2B-4b5563?style=flat-square" alt="Go 1.24.2+">
  <img src="https://img.shields.io/badge/license-MIT-4b5563?style=flat-square" alt="MIT license">
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Linux-4b5563?style=flat-square" alt="macOS and Linux">
</p>

Weft is a local terminal dashboard for agents and shell commands. It keeps parallel work grouped by workspace and highlights tasks that need attention, so you can jump back into the right console.

It is meant for the practical mess of running several agent sessions next to tests, dev servers, logs, scripts, and one-off commands. Instead of scanning terminal tabs to remember which session is waiting, which command is still running, and which project a console belongs to, Weft keeps those tasks visible in one local TUI.

## Demo

https://github.com/user-attachments/assets/76bfa4c7-9398-4d2b-afd1-882816f7ebb5

## Quickstart

Install with Homebrew:

```sh
brew install edwmurph/tap/weft
weft
```

From the dashboard:

- Press `n` to create a task.
- Choose the agent task type or `Shell` for a normal shell.
- Press `Enter` to open the selected task console.
- Press `C-b` to return from a task console to the dashboard.
- Press `?` for shortcuts.

If keyboard input feels wrong in Weft, such as Option/Alt word movement not working or Option+Backspace acting like plain Backspace, run:

```sh
weft doctor keys
```

## Core Ideas

- A workspace is a project directory.
- A task is a local PTY-backed process, usually an agent session or shell.
- Groups are optional flat labels inside a workspace, such as `release`, `review`, `bugs`, or `experiments`.
- The Workspaces pane shows `active` and `needs attention` counts. `needs attention` means the task is not currently active, so it may be ready, stopped, killed, or in an error state.
- The preview pane shows read-only output for the selected task when there is room. Open the task to use the full console.

## What Weft Helps With

- Group related agent sessions, test runs, and dev servers by workspace.
- Run normal shell commands alongside agent sessions.
- See which task needs attention without scanning every terminal.
- Jump back into the full console when a task needs input.
- Keep task PTYs owned by a local supervisor, so the dashboard can detach or reopen without stopping running work.

## Why Not Tmux Or Zellij?

tmux and zellij are excellent terminal multiplexers, and Weft started from the same need to keep many terminal processes reachable. Weft uses a purpose-built Go TUI and local supervisor instead of a tmux runtime dependency, which makes common dashboard updates faster because Weft does not shell out or poll for routine state.

That stack also supports task-specific features directly: workspace/group metadata, attention counts, live previews, agent resume metadata, and dashboard upgrade/re-entry flows. Weft is not trying to replace every terminal workflow; it complements normal shells and multiplexers when you have multiple long-running agent or shell tasks.

## Supported Agents And Commands

Weft supports Codex today.

Additional agents can be added upon request. Config can also define generic shell command tasks, which are useful for tests, dev servers, logs, scripts, or any other command you want to keep visible beside agent work. Feedback on the workflow and docs is welcome in GitHub Issues.

## Learn More

- [Usage](docs/usage.md): dashboard controls, common commands, upgrades, and key diagnostics.
- [Configuration](docs/configuration.md): task types, configured commands, title templates, and title hooks.
- [Technical Notes](docs/technical.md): how Weft works under the hood.
- [Product Specification](spec.md): the living design contract for Weft.
- [Contributing](CONTRIBUTING.md): local checks and release workflow.
- [Support](SUPPORT.md): where to report issues and ask for help.
- [Security](SECURITY.md): local execution model and vulnerability reporting.
