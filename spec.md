# Weft Product Specification

This is the living product and technical specification for Weft. Keep this file current as the product evolves so implementation agents can treat it as the anchor definition.

## Product Definition

Weft is one global terminal dashboard for managing agents and configured shell commands across multiple workspaces. A task is a long-running PTY-backed process. Integrated agent support is checked into Weft; configured command task types are loaded from config. Today Weft ships one supported agent, `codex`, and one default configured command task type, `shell`.

Weft is no longer one instance per workspace. One local Weft supervisor owns the global navigation state, the task registry, and task PTYs. Terminal UI clients attach to that supervisor, render the dashboard, and can detach without stopping tasks. Users can organize tasks by workspace, optionally place tasks into flat groups, then enter a selected task console when they want to interact with it.

The core workflow is:

1. Open Weft.
2. Use the left navigation panes to choose a workspace and task.
3. Press `Enter` to maximize and focus the selected console.
4. Interact with the task only while the task console is focused and maximized.
5. Reopen navigation to switch, organize, create, move, edit, or close tasks.

## Design Principles

- Global first: one Weft should manage all configured workspaces.
- Task first when active: once a task is opened, that PTY gets the whole terminal.
- Navigation is structural, not workflow-stage based.
- Workspace and group movement is manual.
- Group names are flat strings.
- Groups are optional; tasks can live directly in a workspace without a group.
- Task rows render configured text only; no fixed status pills beside each row.
- Integrated agent support is checked into Weft. Config can define generic command task types, but config alone cannot create a new tailored agent integration such as Claude.
- The terminal UI should stay dense, minimal, and close to the current iTerm-style Weft look.
- Supervisor-owned sessions: task PTYs must outlive any single TUI client.
- Disposable clients: closing, upgrading, or restarting a TUI client must not clear state or stop tasks.
- No tmux runtime dependency: tmux must not be required for normal launch, attach, detach, rendering, upgrades, or tests.
- Event-driven speed: avoid polling loops and shelling out for routine runtime state.
- Minimal dependencies: prefer the Go standard library and existing terminal/PTY dependencies before adding new packages.

## Current And Legacy Boundary

Current Weft behavior is the workspace, group, and typed task model backed by one local supervisor. Persisted state is the strict v6 task schema, with `tasks`, `active_task_id`, `selected_task_id`, provider-neutral live title/status and resume metadata, task `terminal_cwd`, and focus values of `workspaces`, `tasks`, or `console`. The supervisor owns all task PTYs, saves current state, captures resume IDs for Codex agent tasks, tracks OSC 7 cwd updates for terminal tasks, detects when on-disk config has drifted from the supervisor's active config, and performs the dashboard `U` upgrade/resume flow through the supervisor-owned `upgrade_resume` IPC command. Runtime behavior for each task type kind is owned by the checked-in task type definition registry, so startup state, input routing, command construction, screen-derived status, loading, terminal cwd tracking, foreground-command tracking, and restart behavior live with the task kind instead of scattered UI conditionals. Resuming Codex agent tasks with `codex resume <session-id>` after an explicit dashboard upgrade or config reload restart is part of the current product contract, not legacy compatibility. Configured command tasks are not resumable by the Codex resume integration; idle terminal tasks can only be restarted as fresh commands with saved history/cwd.

Legacy behavior is unsupported unless this specification explicitly brings it back. Legacy includes tmux pane state, tab/column state, workdir/folder naming, hidden old commands, old config keys, state/config migration paths, and alias support for retired command or state shapes. Legacy files should be rejected with reset guidance rather than migrated, defaulted, repaired, or silently ignored by hidden compatibility code.

## Supported Agents

Weft supports Codex today.

Codex support is the checked-in `kind = "codex"` task type definition. It includes Codex-specific title/status capture, resume ID capture, interrupt routing, command construction for `codex resume <session-id>`, and dashboard upgrade/resume behavior.

Additional agents can be added upon request. New agent support requires a checked-in task type definition; config alone can define generic shell command tasks but cannot add agent-specific behavior.

## Runtime Architecture

Weft has two runtime roles in one shipped binary.

Weft also has a build channel. Release/Homebrew builds set `version.BuildChannel` to `release`; source builds default to `source`. A source build must fail closed before reading or mutating the default `~/.weft` runtime unless it can infer a checkout-local runtime from the current working directory, `WEFT_ROOT` or `WEFT_HOME` is set explicitly, or `WEFT_ALLOW_MAIN_RUNTIME=1` is set for an intentional one-off. Help, version, and `weft doctor keys` remain available without default runtime access.

## Release Version Policy

Weft stays on the `0.x` release line until the maintainer explicitly declares the stable `1.0` release. While `VERSION` is `0.y.z`, release automation treats major bumps as pre-1.0 compatibility bumps and publishes `0.(y+1).0` instead of `1.0.0`. Breaking changes during pre-1.0 are minor releases in the `0.x` line. Patch bumps publish `0.y.(z+1)`, and minor bumps publish `0.(y+1).0`. GitHub releases for `v0.*` tags are marked as prereleases.

Crossing from `0.x` to `1.0.0` requires a manual `Publish Homebrew` dispatch with `bump=major` and `allow_stable_major=true`. Normal pushes to `main` must not automatically create a `1.0.0` release.

Homebrew publishing keeps separate owner and contributor paths. Normal maintainer releases from Weft `main` use a direct tap workflow: the Weft publish workflow writes the formula update to `edwmurph/homebrew-tap` `main`, dispatches the tap direct bottle workflow, waits for an Apple Silicon macOS bottle build, uploads the bottle to the tap GitHub Release, and verifies the formula `bottle do` block. Contributor or manual tap changes still use the standard pull-request bottle flow: the tap `brew test-bot` workflow builds bottles for supported PR runners, and the tap `brew pr-pull` workflow uploads bottles to GitHub Releases, merges the formula update, and writes the formula `bottle do` block. The formula must still keep its source `url`, `sha256`, and Go build instructions so `brew install --build-from-source edwmurph/tap/weft` remains a fallback.

## Release Notes Policy

The publish workflow generates GitHub release notes from the shipped commits. Conventional Commit subjects provide the default user-facing bullet, and `Release-Notes:` body bullets replace that default when the subject is not descriptive enough.

Release metadata commits use plain `Release vX.Y.Z` subjects. They must not add visible CI-skip markers to the commit subject.

Pull request CI validates commit subjects before release automation sees them. Every PR commit must use one of the allowed release-note Conventional Commit subjects: `feat:`, `fix:`, `docs:`, `refactor:`, or `chore:`, with optional scopes such as `fix(tui):` and optional breaking markers such as `feat!:`.

Breaking changes must be grouped first under `Breaking Changes` and must be visibly actionable for users before they upgrade. A breaking ship commit should use a Conventional Commit breaking marker such as `feat!:` or a `BREAKING CHANGE:` footer so the workflow classifies it correctly. The generated breaking-change entry includes the release-note summary, an `Impact:` line from the `BREAKING CHANGE:` footer when provided, and a `Migration:` line from a `Migration:`, `Migrate:`, `Upgrade:`, `Action Required:`, or `How to Migrate:` footer when provided. If a breaking marker is present but migration guidance is missing, the generated notes must still warn users to review the item before upgrading rather than blending the change into ordinary features or fixes.

## Supervisor

The supervisor is a local background process, referred to internally as `weftd`. It is started automatically by `weft` when needed and is scoped by `WEFT_HOME`, or by `$WEFT_ROOT/.weft` when only `WEFT_ROOT` is set. There is at most one active supervisor per runtime directory.

The supervisor owns:

- config loading
- state loading, validation, mutation, and persistence
- the task registry
- task PTY processes
- terminal screen state for each task
- title hook execution
- local IPC over a Unix domain socket
- attached client coordination
- version and protocol negotiation

The supervisor must not listen on a network interface. Its socket lives inside the Weft runtime directory with user-only permissions.

## Clients

The `weft` command is a CLI and TUI client. By default it ensures the supervisor is running, attaches an interactive terminal UI, and exits only the client when the user closes Weft. `weft --no-attach` ensures the supervisor is running and then returns. `--clear` may be combined with launch to force a fresh runtime before launch.

The interactive client owns only terminal rendering, local input collection, and transient modal editing state. Product state changes are sent to the supervisor as commands. The supervisor responds with snapshots and event updates that the client renders.

Only one interactive TUI client owns foreground rendering and input at a time in the first implementation. A second `weft` attach should take over cleanly and cause the previous client to exit with a short message that another client attached. Non-interactive CLI commands such as `weft status` can run concurrently.

When an interactive client attaches, it sets the terminal icon, tab, and window title to exactly `Weft` using standard terminal title sequences.

When `terminal_attention.enabled = true`, the attached interactive client may emit terminal-emulator attention sequences through a terminal-specific provider. Current attention support is iTerm2-only and is detected from the attached terminal environment. Weft must not emit iTerm2-private sequences in other terminals. Weft uses the global unsilenced `needs attention` task set, using the same semantics as workspace cards: active tasks are excluded; ready, sitting, and stopped tasks suppressed by task-level or group-level silence are excluded; killed and error tasks still count. The terminal-attention task set must also treat snapshot loading tasks and terminal foreground-command tasks as active so a configured shell command such as `sleep` notifies only after it returns to the prompt. After the initial snapshot, Weft posts a provider notification whenever an existing task newly enters that attention set, even if other tasks already need attention. Notification text should be concise and include the task title, for example `Tests needs attention`, avoiding shell/session title prefixes when the task has its own title. A task first observed after the client is initialized is treated as user-created and its first attention state is acknowledged without notification. If the active task console is open, that task is also treated as acknowledged and does not notify later merely because focus moves away. When `terminal_attention.request_attention = "once"`, the client also asks the provider to request attention once where supported; `"off"` disables the attention request. The iTerm2 provider must not set iTerm2 session badge text for this feature because that renders as an in-terminal watermark. The iTerm2 provider may emit an empty iTerm2 session-badge sequence only to clear stale badge text left by older test builds.

## IPC

Client and supervisor communication should use a small versioned protocol over the local Unix socket. The protocol should support:

- handshake with binary version and protocol version
- command request and response
- state snapshot response
- event subscription for state, PTY screen, status, and shutdown events
- key/input delivery to the active task PTY
- terminal size updates from the active TUI client
- structured errors suitable for CLI output and TUI footer messages

Every IPC request must include the current `protocol_version`. The supervisor rejects missing, zero, stale, or future protocol versions with a structured `protocol_mismatch` error before applying any command. Current Weft CLI and TUI clients populate the field automatically. Client metadata such as `client_id`, terminal `width`, terminal `height`, launch workspace, and upgrade client executable belongs in typed request fields, not inside command arguments.

The protocol does not need an external RPC framework. New dependencies should be added only if the standard library becomes clearly insufficient.

Command payload contracts are strict. `new` accepts only `title`, `type`, and optional `silent=true|false`; invalid `silent` values return `invalid_silent`. `rename` may include optional `silent=true|false`; omitting it preserves the task's current silent value. `move` accepts only `id`, `direction`, and the current `group` path argument. `task_context_set`, `task_context_show`, and `task_context_clear` accept only the task-context arguments documented by their CLI commands. Transport metadata inside command arguments, such as old `client_id`, `width`, or `height` entries, fails with the same unsupported-argument error as any other unknown command argument.

Client lifecycle commands are supervisor-owned. `attach_client`, `client_detached`, and `close_client` are handled by the supervisor client coordinator rather than by the engine that owns task and workspace state.

## Process And Upgrade UX

Users should not need `weft clear` after upgrades or config reloads that preserve the current state/config contract. Unsupported stale state, config, or IPC shapes fail loudly and may require `weft clear` or config restoration.

When a newly installed `weft` client finds an older compatible supervisor:

- attach to it successfully
- show a concise upgrade banner in the TUI and `weft status`
- clearly distinguish a client-only reopen from a supervisor restart: if the supervisor is still older, reopening the dashboard alone is not enough to finish the runtime upgrade
- show a concise bottom-of-Workspaces-pane tip with the client version, supervisor version, and the `U` upgrade/resume action while the dashboard navigation is open when that action can proceed
- keep existing tasks and PTYs running
- offer the dashboard upgrade action only through the supervisor-owned `upgrade_resume` IPC command

When `config.toml` changes after the supervisor has started, the supervisor keeps using the active in-memory config until a safe restart applies the changed config. A valid changed config is surfaced as the same restart-needed dashboard path as a compatible supervisor upgrade: `weft status` and the Workspaces-pane footer show that config reload is pending or ready, `U` opens the same safe confirmation, and the restart creates a pre-config-reload backup before closing/restarting task PTYs. An invalid changed config must not trigger restart; Weft should show that config reload is blocked until the config error is fixed.

When no tasks are running, Weft may restart the supervisor automatically to finish the upgrade after creating a runtime backup. When any task PTY is running, Weft must not restart the supervisor without explicit confirmation because that can stop live terminal work.

The in-dashboard upgrade/config-reload action must be safe by default. While Codex agent tasks are busy, missing saved resume IDs after user input has been submitted, or any terminal task is running a foreground command, the dashboard shows pending copy, lists blocking tasks as YAML-style workspace/task entries using resolved task display titles rather than stored title templates, and does not offer `U`. A live Codex agent task that has not submitted input yet and has no resume ID is safe to recreate as a fresh agent task after restart. An idle/ready terminal task with no active foreground process is safe for explicit `U`; this is not shell resume. Once every remaining live task is either an idle resumable Codex agent task, a fresh unsubmitted Codex agent task, or an idle terminal task, `U` opens a confirmation where `Enter` proceeds and `Esc` cancels. The confirmed action creates a pre-upgrade or pre-config-reload backup, preserves task rows, saves read-only terminal task history snapshots, closes idle task terminals, restarts the supervisor, resumes Codex agent tasks with `codex resume <session-id>`, starts fresh Codex agent tasks without a resume ID, and restarts idle shell task(s) with saved history/cwd. Terminal jobs, environment mutations, shell variables, and unsubmitted input are not preserved. The client must not run duplicate local restart/resume logic or synthesize upgrade/config state that was not sent by the supervisor. An already-open dashboard client reloads its local config only after the replacement supervisor reports the new active config fingerprint.

If the supervisor protocol is incompatible with the client, the client should explain the situation and offer the least destructive recovery path:

```text
Weft was upgraded, but the running supervisor is too old for this client.
Restarting the supervisor will stop running Codex terminals. Saved layout and
metadata will remain.
```

`weft clear` remains a destructive last-resort reset. It must not be presented as the normal upgrade path.

## Runtime Files

Weft stores runtime files globally under `~/.weft` by default, under `$WEFT_ROOT/.weft` when `WEFT_ROOT` is set, under `WEFT_HOME` when set, or under a checkout-local `.weft-runtime` directory for source-checkout auto-rooted runs:

- `config.toml`
- `state.json`
- `task-context.json`
- `weft.sock`
- `weftd.pid`
- `weftd.lock`
- `weftd.log`
- `backups/`

`WEFT_ROOT` sets both development/worktree paths from one value: runtime files go in `$WEFT_ROOT/.weft`, and the launch workspace is `$WEFT_ROOT`. When source-built Weft runs from a Weft source checkout or detached worktree without `WEFT_ROOT` or `WEFT_HOME`, the current working directory is treated as that root and runtime files go under `.weft-runtime`. This keeps `go -C /path/to/weft-or-worktree run ./cmd/weft ...` isolated to `/path/to/weft-or-worktree/.weft-runtime` without requiring an environment override and without touching `.weft/config.toml` symlinks created for other workflows. `WEFT_WORKSPACE` overrides only the launch directory used for attach-time workspace context. `WEFT_HOME` overrides only the runtime directory. Development and worktree runs should usually rely on checkout-local auto-rooting. The installed release command owns the real default `~/.weft` runtime.

Codex task PTYs receive `WEFT_HOME=<runtime dir>`, `WEFT_TASK_ID=<task id>`, `WEFT_TASK_TYPE_ID=<task type id>`, and `WEFT_TASK_KIND=codex` so commands running inside that task can address their own supervisor-owned metadata. Configured shell task PTYs do not receive these task-context environment variables.

Runtime backups live under `backups/<id>/` by default. A backup includes `metadata.json`, `config.toml` when present, `state.json` when present, `task-context.json` when present, and log files when present. Backups must not include sockets, locks, pid files, or live PTY/process state.

## Development Worktree Hygiene

Repository-local development worktrees live under the primary checkout's `.worktrees/` directory and are created with `scripts/create-worktree.sh <slug>`. Source runs from those worktrees should use `go -C <worktree> run ./cmd/weft ...`; source-checkout auto-rooting keeps each worktree's runtime, config, supervisor socket, pid file, state, and logs under `<worktree>/.weft-runtime`.

`scripts/cleanup-worktrees.sh` is the destructive cleanup path for disposable auxiliary worktrees. It targets only Git-registered worktrees under `.worktrees/`, preserves the primary checkout and registered external worktrees, stops each target's `.weft-runtime` and `.weft` supervisors when present, removes the worktree, and prunes stale Git worktree metadata. The script shows a plan and asks for confirmation by default; `--dry-run` previews the same plan without changing anything, and `--yes` confirms the cleanup for unattended use.

## Primary Layout

The app has three logical panes.

## Workspaces Pane

The left pane lists configured workspaces as vertically stacked bordered cards in their persisted manual order. When the Workspaces pane is focused, `Shift+Up` and `Shift+Down` reorder the selected workspace card.

When there are no configured workspaces, the pane shows centered help text telling the user that there are no workspaces and to press the configured add-workspace key.

When at least one workspace exists and there is enough vertical room, render a template card under the last workspace card with an italic plus-sign title and concise italic copy telling the user to press the configured add-workspace key to create a workspace. Hovering or clicking the template card selects it: real workspace cards return to their inactive border, and the Tasks pane renders the same empty state as no selected workspace while Task Live Preview renders `No task selected`. The template card is also selectable by moving down from the last workspace card. Pressing `Enter` while the template card is selected opens the same add-workspace prompt as the configured add-workspace key. If that prompt is canceled, focus returns to the selected template card instead of jumping to a real workspace.

Stored workspaces remain selectable even when their path is missing, unreadable, or no longer a directory. In that bad-state case the card shows a visible warning line such as `path missing; press Backspace to remove`, using the configured delete key, so the user can navigate to the entry and remove it without resetting all state.

Each card renders:

- a title in the top border
- `total`, the number of all tasks in that workspace
- `active`, the number of tasks whose rendered/live status is `starting`, `running`, `waiting`, `working`, or `shipping`
- `needs attention`, the number of non-active tasks that are not silenced and still require attention
- `silenced`, the number of ready, sitting, or stopped non-active tasks suppressed by either task-level or group-level silence

Do not render card-level `parked`, `stopped`, `killed`, `quiet`, or `error` categories. Those task states remain available to title templates and other task-level surfaces, but the Workspaces pane summarizes them only through `active`, `needs attention`, and `silenced`.

The default card title is the display path, for example `~/code/personal/weft`. A workspace can also have an optional manual title override. When the override is non-empty, the card uses that title instead of the path. Blank edit input clears the override and returns the card to the default path title.

Selection is indicated by the card border, not a full-row background. Use a stronger blue border when the Workspaces pane has focus. Use a subtler blue border when the selected workspace is active but focus is in the Tasks pane.

When a newly installed client is attached to an older compatible supervisor, or when `config.toml` has changed since the supervisor started, the bottom of the Workspaces pane shows a concise restart tip. While any Codex agent task is still active, the tip waits for idle/resumable Codex agent tasks and lists blockers as `Blocking:`, `- workspace: Core`, `  task: Release fix`. While any terminal task is running a foreground command, the tip waits for idle shell task(s) and uses the same blocker-list shape, for example `Blocking:`, `- workspace: API`, `  task: Server`. Config drift uses config-specific copy, for example `Config pending: config.toml changed`, `Wait for 1 shell task(s) to become idle`, `Blocking:`, `- workspace: API`, `  task: Server`. When all remaining Codex agent tasks are idle and have saved resume IDs, or are fresh Codex agent tasks with no submitted input, and all remaining terminal task(s) are idle, the tip shows the action, for example `Upgrade ready: supervisor 7.4.0 → 7.5.5. Press U to upgrade and resume 2 idle Codex task(s) and restart 1 idle shell task(s) with saved history/cwd.` Config drift ready copy says `Config ready: config.toml changed. Press U to apply config and ...`. For fresh unsubmitted Codex agent tasks without a resume ID, the ready copy says Weft will start fresh Codex agent task(s) after restart instead of resuming them. The tip must not imply that reopening the dashboard is enough to finish the upgrade or config reload, and it must not suggest destructive reset commands while live tasks can be resumed. The Tasks pane should not duplicate this pending/ready copy during an upgrade or config reload. The confirmation modal explains that Weft closes idle Codex terminals, restarts the supervisor, runs `codex resume <session-id>` for each saved Codex agent task, starts fresh Codex agent tasks that do not have a resume ID, and restarts idle shell task(s) with saved history/cwd. It also warns that terminal jobs, environment mutations, shell variables, and unsubmitted input are not preserved.

When there is enough vertical space, the top of the Workspaces pane shows compact runtime branding and version details inside a small centered box with sharp corners and compact padding. The box uses a small emphasized `Weft` mark followed by the current CLI version and running supervisor version, with the version values aligned in one column. This header stays visible while an upgrade tip is active. The upgrade tip remains pinned to the bottom of the Workspaces pane, and the workspace-card body between the header and footer scrolls with the selected workspace as keyboard arrows move through the list. Workspace cards render one blank line below the version box when vertical space allows it. This header is secondary chrome: it must not permanently hide workspace cards.

Counts should use subtle colors:

- `total`: muted neutral
- `active`: blue
- `needs attention`: the Tasks pane ready highlight/text yellow when nonzero, muted neutral when zero
- `silenced`: muted neutral

Example:

```text
╭ ~/code/personal/trading-engine ─────────────────────────────╮
│  8 total        3 active        5 needs attention            │
╰──────────────────────────────────────────────────────────────╯
```

## Tasks Pane

The middle pane shows tasks for the selected workspace. It is always present so the Workspaces pane can stay purely scoped to workspaces.

When no workspace exists or no workspace is selected, the Tasks pane shows centered help text telling the user to add a workspace first. It must not advertise creating a task until a workspace exists. When a workspace is selected, the top of the Tasks pane renders a selectable italic template row with a plus-sign label and concise copy telling the user to press the configured new-task key to create a task. Pressing `Enter` while the template row is selected opens the same new-task form as the configured new-task key.

Tasks without a group render as top-level rows above group sections. User-created groups render as collapsible sections inside this pane, with their member tasks indented underneath. Creating a group must not force existing top-level tasks into a visible `Ungrouped`, `General`, or `Inbox` section.

Group names are plain text. Emojis are inherently allowed because the group name is just user text. Weft does not need a separate emoji feature, picker, or icon system for groups in the first implementation.

Group names are flat. Valid group examples:

```text
dashboard
release
client-followups
🧪 ideas
```

Nested group names are out of scope for the first implementation. Treat strings containing `/` as invalid group names unless this spec is updated.

Each group row renders:

- group name text
- number of tasks in the group

Each task row renders:

- marker shape, task type badge, and rendered task title template

Task rows must not render fixed status tags. Status can appear only if the task title template includes a status variable.

Task rows may use subtle row color and marker shape to make derived state easier to scan. Rows for active non-ready/non-idle task states (`starting`, `running`, `waiting`, `working`, `shipping`, or newer live task states) replace the static marker with the shared high-resolution Braille loading spinner frame. Rows whose PTY has not produced visible content may also use the spinner until the task is ready. Configured command task rows also use the same spinner while a submitted foreground command is in progress, returning to the ready marker when the shell regains the PTY foreground process group. Codex screens detected as ready user prompts, including tool permission prompts and command approval prompts, use the ready marker instead of the loading spinner. Unsilenced ready or idle rows use the subtle `·` marker and keep their ready color even when selected, hovered, or also the active task; the selected ready-row variant must use enough foreground/background contrast to stay readable. Ready, sitting, or stopped rows suppressed by task-level or group-level silence use the muted task-row color instead of the ready or attention color unless the cursor is actively selecting the row. Stopped rows use `◦`; errors use `!` and the error marker/color. Task-level silent rows render `⊘` after the task state marker and before the task type badge, such as `· ⊘ [codex] Title`. These visuals are presentation only and must not add status text beyond the silent marker.

Task type badges render as plain bracketed text such as `[codex]` or `[shell]`, usually from the task type ID, in a fixed-width badge column so task rows align across terminal fonts. Avoid emoji and wide symbols because terminal width and fallback-font behavior is inconsistent.

When there is enough room for all three panes, the Tasks pane should prefer a 54-column frame before giving extra columns to `Task Live Preview`.

Group rows should be visually distinct from task rows. Use the chevron/collapse marker, count, stronger color or weight, and one blank line before group sections. Keep the group name visually stronger than the inline count so the count reads as supporting metadata. When there are no top-level tasks, the first group must reuse the new-task template row's existing separator instead of adding a second blank line. Task rows should use a lighter marker and indentation when nested under a group.

Groups render in the persisted manual order stored in state for that workspace. They are not sorted alphabetically.

When a collapsed group contains an active/loading task, the group row surfaces the shared loading spinner after the chevron so hidden terminal foreground commands and other active child tasks are still visible in the Tasks pane.

When the Tasks pane has more rendered rows than fit in the visible frame, moving the cursor must scroll the pane enough to keep the selected group or task row visible.

The Tasks pane cursor is persisted separately from the active task console. Moving the cursor to a group row must survive supervisor refreshes, restarts, and upgrades without snapping back to the active task inside that group.

Moving focus left to Workspaces, changing the selected workspace, returning to a workspace that contains the active task, and then moving focus right to Tasks must resume the Tasks-pane cursor at that task. The next Tasks-pane movement continues from that row instead of restarting from the new-task row or first group.

`Shift+Up` and `Shift+Down` reorder the selected workspace, task, or group row. On a selected workspace card, the workspace moves among the other workspaces. On a selected task row, the task moves within its current group or top-level area when possible. At an area boundary, the task moves into the adjacent area: `Shift+Down` from the last top-level task moves it to the top of the first group, and `Shift+Up` from the first task in a group moves it to the end of the previous group or top-level area. Task and group reordering never changes the workspace and does not restart the task PTY. On a selected group row, the whole group section moves among groups in the same workspace. Top-level ungrouped tasks remain above group sections.

## Task Live Preview And Console

The main task pane has two modes:

- `Task Live Preview` when command center navigation is open
- `Task Console` when the selected task is focused and maximized

The pane shows either:

- a centered empty message when no task is open, with a subtle Weft wordmark when space allows. Dashboard version information belongs in the Workspaces pane header, not in the task pane empty state.
- the selected task terminal when a task is open

When navigation is open, the Workspaces and Tasks panes push `Task Live Preview` to the right. The preview shows live task output only when the current navigation focus is on a task row, or when a real workspace card is focused and a task remains selected in that workspace. Focusing the new-workspace template card, a group row, or any other non-task navigation target renders `No task selected` instead of the last viewed task. If the focused task row and the captured task output owner disagree after a move, hover, refresh, or supervisor restart, the preview renders `No task selected` until the next synced snapshot instead of showing another task's output. When a workspace already has tasks but the current target is not a task row, the `No task selected` hint tells the user to select a task to preview rather than suggesting task creation. The `No task selected` state uses the shared Weft wordmark with balanced diamond input nodes, a centered solid output arrowhead, visible spacing before the block text, and a subtle faster left-to-right pulse in fixed-width chunks limited to the arrow graph, followed by a roughly three-second pause before the next pulse. That animation is presentation only and must not imply task activity. When a task row is focused, the preview title appends one space and a slowly pulsing dot to indicate the preview can update with live task output. The preview title animation is also presentation only and does not mean the selected task is busy; it is omitted when there is no selected task to preview. The preview is read-only: keyboard input controls Weft navigation and organization, not the task PTY. Trackpad or wheel scrolling inside the preview frame scrolls Weft's captured scrollback for the task and does not forward mouse packets into the task PTY. Left-button drag selection inside the preview uses the same selected-cell highlight, clipboard copy behavior, and brief copy-confirmation toast as `Task Console` without changing navigation focus. When a task row is focused, the preview top border shows the selected task title at the top right, except while the copy-confirmation toast is visible. Preview content reserves one inner column on both the left and right, and clipped terminal lines use a subtle reserved right-edge marker before the right padding so the pane reads as a live cropped lens instead of a full interactive terminal.

When the user presses `Enter` on a task, navigation slides away left, `Task Console` expands to the full terminal, and focus moves to the task console.

Task PTYs can only receive input when `Task Console` is focused and maximized.

When `Task Console` is focused, the top border shows the configured drawer key as `<key> dashboard` and the configured Task Tools key as `<key> tools` without a `WEFT` prefix, and the top-right border shows only the active task title. If task notes are enabled and the active task is a Codex task with a stored short note, the focused console shows a subtle `note` badge and the clipped one-line note beside the left toolbar. Multi-line notes never render as always-visible task output; they are shown when Task Tools is opened with the configured tools key. Task notes do not render in `Task Live Preview` or configured shell task consoles. If at least one other global unsilenced task has rendered/live status `ready`, the bottom-right border shows an amber `<n> other task(s) ready` indicator. The active console task and silenced tasks are excluded from that count, and the indicator is hidden when no other unsilenced tasks are ready. Other short console notices, including copy-confirmation toasts, also render in the bottom-right border so the task title remains the only top-right console item. The active `Task Console` pane border remains the active Weft blue on every border segment and corner even when bottom-right notices use their own text styling.

## Navigation States

Weft has two primary UI states.

## Dashboard State

Navigation panes are open.

- Workspaces pane is visible.
- Tasks pane is visible.
- `Task Live Preview` pane is visible but not focused when terminal width allows it.
- Task PTY does not receive normal key input.
- User can create, delete, edit, move, and select objects.

## Dashboard Form UX

All in-dashboard text-entry forms use the same compact modal treatment:

- rounded, bordered input directly below the field label
- one compact status or validation line below the input or suggestion menu
- short state-specific footer actions, such as `Enter save`, `Tab choose`, `Esc close suggestions`, and `Esc cancel`
- footer action labels use arrow glyphs such as `↑/↓`, `←/→`, and `↓` instead of words such as `Up/Down`, `Left/Right`, and `Down`
- `Enter` submits only when the current value is valid for that form; invalid required values keep the form open and show the validation status
- when autocomplete is open, `Enter` chooses the highlighted suggestion; form submission is available only after autocomplete is closed
- `Esc` closes an open autocomplete menu first; otherwise it cancels the form
- prompt inputs support Option/Alt word movement and deletion when the terminal sends Option as Meta/Esc

All in-dashboard confirmation modals use the same keyboard contract:

- `Enter` accepts the affirmative action, such as yes, delete, stop and delete, or upgrade
- `Esc` cancels or declines the action
- `Y`/`N` are not part of the dashboard confirmation flow

Autocomplete appears only when it has a useful known set:

- path autocomplete for the add-workspace path prompt
- group-name autocomplete for moving a task to an existing group

Autocomplete matches case-insensitive substrings in the relevant path segment or group name, not only prefixes from the beginning of the value.

Autocomplete menus open directly under the input whenever the current value has suggestions, including on prompt initialization. The visible options update as the user types, use a bounded visible row count, and scroll to keep the highlighted option visible. Choosing an autocomplete option with `Enter` or `Tab` closes the menu and leaves the form in a normal submit state.

## Task Focus State

`Task Console` is maximized.

- Workspaces and Tasks panes are hidden offscreen to the left.
- `Task Console` fills the terminal.
- Weft keeps the framed `Task Console` pane visible while a task is focused.
- The attached client forwards raw terminal input bytes into the active task PTY without key-name reconstruction. The configured drawer key, `C-b` by default, and the configured Task Tools key, `C-]` by default, are owned by Weft.
- Terminal-generated C-c belongs to the task whenever `Task Console` is focused and an active task exists. For configured terminal tasks with an active foreground command, Weft forwards C-c as the normal terminal interrupt byte. For configured terminal tasks at an idle prompt, Weft kills the task PTY, returns to the dashboard `Tasks` pane, and reports the task as killed. For Codex agent tasks, while Codex reports active work, Weft delivers C-c through Codex's interrupt path so running side-thread work is interrupted without returning from or closing the side thread. Weft does not use C-c to quit from `Task Console`, and the toolbar must not advertise C-c.
- Terminal-owned behavior, including Vim mode, Esc timing, bracketed paste, Alt/Meta prefixes, and modified-key shortcuts such as Shift+Enter and Shift+Tab in supporting terminals, is preserved inside the framed pane.
- The framed terminal renderer preserves cursor visibility and cursor shape requests, including block, underline, and bar cursor modes used by Vim insert/normal state.
- Weft enables cell-level mouse tracking in the attached client. In focused `Task Console`, trackpad or wheel scrolling anywhere in the console frame moves through Weft's captured scrollback for normal shell output. When a focused configured terminal task is in terminal full-screen or pager mode, indicated by alternate-screen terminal state, trackpad or wheel input is forwarded to that task PTY so pager/full-screen programs can handle it interactively. `Task Live Preview` remains read-only and always treats wheel input as Weft-captured scrollback. Terminal task preview renders as a cropped lens and must not destructively narrow saved terminal rows when the dashboard drawer opens or reattaches after an upgrade. Codex task drag selection starts after the shared visual indentation margin, while configured terminal task drag selection starts at terminal column zero so the highlighted cells and copied clipboard text can include the full row without post-copy text rewriting. While the drag highlight is active, highlighted cells use one consistent foreground color and one consistent background color, regardless of the task's existing cell colors. The console border shows a short copy-confirmation toast. Mouse input outside the focused console remains a Weft dashboard concern and is not forwarded to Codex.
- If the active task PTY exits while `Task Console` is focused, Weft returns to the dashboard `Tasks` pane, keeps the task selected, and makes the exited state visible in task metadata. Normal exits are reported as stopped; exits immediately after a forwarded C-c are reported as killed.
- User exits back to dashboard with the configured drawer/navigation key.

## Initial Keybindings

These are product-level defaults and may map to existing config structures during implementation.

```text
Enter   Open selected task and maximize its console, or open the new-task form on the new-task template row
C-b     Toggle dashboard navigation
C-]     Open Task Tools
Left/Right Move focus between workspaces and tasks panes
j/k     Move selection within the focused navigation pane
w       Add workspace
g       Create group in selected workspace
n       Open the new-task form
m       Move selected task to another group in the same workspace, or clear its group
Shift+Up/Down Reorder selected workspace, task, or group
e       Edit selected workspace title, group, or task title
Backspace Delete/remove selected item
?       Help
C-c     Quit Weft from dashboard focus
```

While Help is open, `C-r` repaints the whole attached client screen and refreshes the dashboard snapshot without closing Help.

Task Tools shows task notes and console commands in separate sections. The Task Notes area uses most of the available space and shows the concise note as the lead line, followed by wrapped longer notes when present, without separate field labels that repeat the section purpose. Left-button drag selection inside that section is bounded to the notes body and copies selected note text to the clipboard. The Console Commands section offers `r` to repaint the attached client and refresh the dashboard snapshot, `c` as `Copy full task console` to copy the current task console's plain captured content to the clipboard for debugging, `Enter` to run the selected command, and `Esc` to close.

Deletion behavior depends on selected item type and is defined below.

## Status

Task status exists in the model. The Workspaces pane summarizes status only as `active`, `needs attention`, and `silenced` counts per workspace. Beyond the console-only ready indicator, a separate top-level global status summary is deferred.

Status should be available to title templates.

Initial statuses:

```text
starting
running
ready
sitting
shipping
stopped
killed
error
```

The exact derivation of `ready`, `waiting`, `running`, and other live states is owned by each task type definition and can evolve independently of the UI layout. For Codex tasks, the Codex definition parses live terminal-title status into provider-neutral `live_status`, preserving status-word casing including newer labels such as `Exploring` or `Crafting`; fallback lifecycle statuses remain the lowercase model values above. When the Codex screen is stopped on a user prompt, such as Plan mode waiting for a user answer, a tool permission allow/deny choice, or a command approval prompt, Weft derives `Ready` for `{status}` even if the terminal title has not changed from a running-like title.

Runtime behavior must resolve provider-specific or command-specific live words into consolidated buckets before making task-pane decisions. Active buckets are `starting`, `running`, `waiting`, `working`, and `shipping`; ready and terminal buckets are `ready`, `sitting`, `stopped`, `killed`, and `error`. Unknown live work words such as `Crafting` or a configured terminal command's custom active label are treated as `working` for row styling, active counts, interrupt routing, and task-pane duration timing. The task-pane duration prefix is derived from transitions into and out of those consolidated active buckets, not from provider-specific display text.

## Task Types

Task types are loaded from config. Each task type represents either an agent or a configured shell command. Each task type has:

- `id`: map key, such as `codex`, `shell`, or `logs`
- `label`: human-readable display label
- `kind`: either a checked-in agent kind or `terminal`
- `command`: shell command used to start the PTY
- `badge`: bracketed type badge rendered before the task title; when omitted, it defaults to `[<id>]`
- `title_template`: default title copied into newly created tasks of this type

Each `kind` resolves to a checked-in task type definition. Definitions own the task-kind capabilities that affect runtime behavior: input mode, startup status, command construction, screen-derived status, loading rules, terminal cwd tracking, foreground-command tracking, exit footer behavior, screen resize behavior, and restartability during dashboard `U`. Checked-in agent kinds can add tailored behavior. `codex` is currently the only supported agent kind. Additional agents can be added upon request. Generic configured command task types must use `kind = "terminal"` and do not get agent-specific live title/status capture, resume ID capture, interrupt routing, or true resume. Unsupported agent kinds, including `kind = "claude"` before Claude support is checked in, must be rejected at config load with guidance to use `terminal` for generic commands. Any idle/ready `kind = "terminal"` task with no active foreground process can be restarted after dashboard `U` with read-only pre-upgrade history and the latest cwd captured from OSC 7.

Default task types:

```toml
default_task_type = "codex"

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

Task context defaults to enabled:

```toml
[task_context]
enabled = true
```

Task notes have two independent fields. The short note is a concise one-line note shown in the focused Codex Task Console heading. The detail field accepts longer multi-line text and appears in Task Tools. Task notes do not render in Task Live Preview or configured shell task consoles.

The dashboard new-task form has focused Type, `[ ] Silent`, and Title fields in that visual order. The form opens with Type focused so choosing between configured task types is the first interaction. `Up`/`Down` move between fields, `Tab` cycles fields, and `Enter` creates the task when the type dropdown is closed. Focused Type and Title inputs use the blue modal input border; focused Silent renders only the `[ ]` or `[x]` checkbox glyph in blue. The Type field renders the selected task type label only, such as `Codex` or `Shell`; `Left`/`Right` cycles task types, and `Space` opens a dropdown where `Up`/`Down` choose a task type and `Enter` or `Tab` closes the dropdown. `Space` toggles Silent when the checkbox is focused. The title input defaults to the selected task type's `title_template`, and supported title variables render under the title input. Changing the selected task type updates the title input to the newly selected type's default only while the input is blank or still matches the previous type default; once the user edits the title, type changes preserve that custom value. The edit-task form renders its Silent checkbox above the Title input, initializes it from the selected task, lists the same supported title variables under the Title input, and title-only command-line rename preserves the current silent value. The Tasks pane reserves a fixed badge column wide enough for the configured task type badges so task rows do not drift out of alignment.

The dashboard `n` shortcut opens the new-task form. `Enter` creates a top-level task of the selected type with the entered title. The CLI command `weft new` creates the configured `default_task_type`, and `weft new --type <id>` creates a specific task type. Tasks always start in the selected workspace and are created top-level with no group.

## Title Templates

Task rows render each task's stored title template. A task type's `title_template` is the default copied into new tasks of that type.

A task title can include template variables. Renaming a task edits that task's stored template and does not change the task type default.

Default Codex agent task template:

```text
{live}
```

Supported variables:

```text
{title}    user-configured task title
{auto}     generated title from the first submitted message
{live}    live task title
{status}   live task status, falling back to lifecycle status
{workspace}  display workspace path
{group}   flat group name
```

The retired `{codex}` variable is unsupported; use `{live}` for the live task title.

Example task templates:

```text
{title}
{live}
{auto}
{status} {auto}
{status} {title}
{group}: {title}
{workspace} / {group} / {title}
```

If a variable is unavailable, render a stable fallback rather than an empty broken string:

```text
{title}  -> ...
{auto}   -> waiting for first message
{live}  -> ...
{status} -> unknown
```

Generated titles are computed when `title_hook_command` is configured and a task opts into `{auto}` titles. Integrated agent tasks capture the first non-empty submitted message. Configured command tasks capture the first typed command after their task type title template includes `{auto}`. The hook runs from the task workspace, sends JSON on stdin, and stores the first non-empty stdout line as the task's generated title. The hook payload version is `3` and includes `version`, `event`, `task_id`, `workspace`, `group`, `status`, `title`, the task `type_id`, task `title_template`, `live_title` when available, `first_message`, `title_columns` for the rendered title area, and `auto_title_columns` for the available `{auto}` text after Weft accounts for the marker, widest configured task type badge, nesting, and other title-template fields.

Weft must not encode provider-specific clients, model names, API keys, or HTTP contracts into the runtime. The title hook is just a shell command. If the hook times out, exits nonzero, is missing, or writes no title, `{auto}` renders as `auto title failed` and Weft does not retry for that task.

When `{auto}` is added in the edit pane, the UI should make hook readiness obvious. If `title_hook_command` is missing, show a configuration error. If the title is already generated, explain that it is ready. Otherwise, explain that auto title generation will run after the first submitted message. Hook failures should be saved on the task and shown in the footer and edit pane.

## Data Model

The state model uses global workspaces, flat groups, and typed task rows persisted as `tasks` in strict v6 state. Unsupported old state shapes fail with reset guidance instead of being migrated, archived, or repaired.

Current persisted shape:

```go
type State struct {
    Version             int         `json:"version"`
    ActiveTaskID        string      `json:"active_task_id,omitempty"`
    SelectedTaskID      string      `json:"selected_task_id,omitempty"`
    SelectedWorkspaceID string      `json:"selected_workspace_id,omitempty"`
    SelectedGroupID     string      `json:"selected_group_id,omitempty"`
    Focus               Focus       `json:"focus"`
    NavOpen             bool        `json:"nav_open"`
    Workspaces          []Workspace `json:"workspaces"`
    Groups              []Group     `json:"groups"`
    Tasks               []Task      `json:"tasks"`
    CollapsedGroupIDs   []string    `json:"collapsed_group_ids,omitempty"`
}

type Workspace struct {
    ID        string `json:"id"`
    Path      string `json:"path"`
    Title     string `json:"title,omitempty"`
    CreatedAt string `json:"created_at"`
    UpdatedAt string `json:"updated_at"`
}

type Group struct {
    ID          string `json:"id"`
    WorkspaceID string `json:"workspace_id"`
    Path        string `json:"path"`
    Silent      bool   `json:"silent,omitempty"`
    CreatedAt   string `json:"created_at"`
    UpdatedAt   string `json:"updated_at"`
}

type Task struct {
    ID                  string     `json:"id"`
    WorkspaceID         string     `json:"workspace_id"`
    GroupID             string     `json:"group_id"`
    TypeID              string     `json:"type_id"`
    Title               string     `json:"title"`
    Silent              bool       `json:"silent,omitempty"`
    AutoTitle           string     `json:"auto_title,omitempty"`
    AutoTitleAttempted  bool       `json:"auto_title_attempted,omitempty"`
    AutoTitleError      string     `json:"auto_title_error,omitempty"`
    LiveTitle           string     `json:"live_title,omitempty"`
    LiveStatus          string     `json:"live_status,omitempty"`
    ResumeID            string     `json:"resume_id,omitempty"`
    InputSubmitted      bool       `json:"input_submitted,omitempty"`
    TerminalCWD         string     `json:"terminal_cwd,omitempty"`
    Status              TaskStatus `json:"status"`
    CreatedAt           string     `json:"created_at"`
    UpdatedAt           string     `json:"updated_at"`
}
```

Runtime-only details such as PID, PTY handles, socket clients, terminal size, and screen cache must not be persisted in `state.json`. They belong to the supervisor process and can be reconstructed from state and live PTYs. Every persisted task row must include a non-empty `type_id`, title, supported status, timestamps, and valid workspace/group references. Persisted state is validated as-is and must not be repaired on load or write. A missing task type or a task `type_id` that is not defined in active config is a startup error with reset guidance.

Task type defaults belong in config and are copied into new tasks:

```toml
default_task_type = "codex"

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

title_hook_command = ""
title_hook_timeout_seconds = 10

[terminal_attention]
enabled = false
request_attention = "once"
```

## Focus Model

Focus values:

```text
workspaces
tasks
console
```

Rules:

- `workspaces` and `tasks` focus are valid only while navigation is open.
- `tasks` is the persisted/internal focus value for the visible `Tasks` pane.
- `console` focus is valid only while `Task Console` is maximized.
- Opening a task with `Enter` sets focus to `console` and closes navigation.
- Reopening navigation moves focus back to the last focused navigation pane.
- Task PTY input is blocked unless focus is `console`.

## CRUD Semantics

## Workspace CRUD

Add workspace:

- User provides an existing filesystem path.
- Weft must not auto-add or reselect the launch directory during state load or first supervisor start.
- When an interactive client opens from a launch directory that is already configured, Weft selects that workspace automatically.
- Launch-directory selection is a first attach behavior for that client. Repeated attach or retry requests from the same attached client must not reselect the launch workspace after the user has navigated elsewhere.
- Launch-directory selection happens only when the interactive client attaches. It must not run on every snapshot, navigation, or delete request, because that would keep snapping selection back to the launch workspace and prevent removing stale workspace entries.
- When an interactive client opens from a launch directory that is not configured, Weft asks whether to add it before mutating state. `Enter` confirms; `Esc` declines.
- Dashboard prompt opens with the selected workspace's parent directory prefilled.
- Dashboard prompt uses the shared bordered form input.
- Autocomplete opens directly below the prefilled input on prompt initialization when matching directories exist, then updates as the user types or presses `Down`.
- While autocomplete is open, `Up` and `Down` move the highlighted option, `Enter` chooses it, and `Esc` closes the menu.
- Autocomplete uses a bounded visible menu; moving past the visible rows scrolls the menu to keep the highlighted option visible.
- When autocomplete is closed and the path is an existing directory, `Enter` adds the workspace.
- Choosing an autocomplete option closes the menu; nested directories do not reopen until the user types or asks for options again.
- Dashboard prompt shows a compact status line for the current path, including an unobtrusive success indicator for existing directories.
- Prompt inputs support Option/Alt word movement and deletion, including Option-Left, Option-Right, and Option-Backspace, when the terminal sends Option as Meta/Esc rather than plain Backspace.
- CLI validation reports missing paths and file paths before adding.
- Store the absolute path.
- Display path using `~` when possible.
- Do not create a default group.

Remove workspace:

- Remove the workspace from Weft state.
- Close all running tasks in that workspace.
- Stop their PTYs.
- Remove associated groups and tasks from state.
- Do not delete filesystem contents.
- Confirm before removal if any task is running, ready, or shipping.
- Dashboard delete confirmations use `Enter` to remove and `Esc` to cancel.

Rename workspace title:

- Workspaces are still identified by path and stable ID.
- Pressing `e` while focused on the Workspaces pane opens a workspace-title prompt.
- Non-empty input stores the optional workspace title override.
- Blank input clears the override and returns display to the default path title.
- No CLI command is required for workspace title changes in the first implementation.

## Group CRUD

Create group:

- Creates a flat group name inside the selected workspace.
- Name must be unique within that workspace.
- Name may include emoji and normal Unicode text.
- Name must not contain `/` in the first implementation.

Rename group:

- Updates the flat group name.
- Keeps all tasks in that group.
- Must remain unique within the workspace.

Delete group:

- Allowed only when empty.
- If non-empty, prompt the user to move tasks first.
- Deleting the last group in a workspace is allowed.
- Dashboard confirmation uses `Enter` to delete and `Esc` to cancel.

## Task CRUD

Create task:

- Requires an existing selected workspace.
- Creates a task in the selected workspace.
- Always creates a top-level task with no group, even when the cursor is on a group or grouped task.
- Uses the selected task type from the dashboard new-task form or `weft new --type <id>`.
- Starts the task type command with the task workspace as the process working directory.
- Copies the task type title template into the task title unless an explicit title is provided.

Rename task:

- Opens with the stored task title template.
- Updates the task title template.
- Does not change the task type default title template.

Move task:

- Moves the task to another group in the same workspace.
- Can also clear the group, moving the task back to a top-level row.
- Dashboard prompt autocompletes existing group names in that workspace, including on blank prompt initialization and after the user types any matching substring.
- Does not restart the PTY.
- Cross-workspace moves are out of scope for the first implementation.

Reorder workspace, task, or group:

- When a workspace card is selected, `Shift+Up` and `Shift+Down` reorder that workspace among the other workspaces.
- Workspace reordering preserves that workspace's groups, tasks, title override, current task/group selection, and running terminals.

- `Shift+Up` and `Shift+Down` reorder the selected task within its current group, or within the top-level ungrouped area when the task has no group.
- At a boundary, task reordering crosses into the adjacent top-level/group area. Moving down from the last top-level task inserts it at the top of the next group; moving up from the first grouped task inserts it at the end of the previous group or top-level area.
- Reordering never changes the workspace and does not restart the PTY.
- When a group row is selected, `Shift+Up` and `Shift+Down` reorder that whole group section among the other groups in the same workspace.
- Group reordering preserves the group's tasks, collapse state, silent flag, and running terminals.

Close/delete task:

- Confirmation explains that deleting stops the terminal before removing the task from Weft.
- Dashboard confirmation uses `Enter` to delete and `N` or `Esc` to cancel.
- Stops the PTY if running.
- Removes the task from state.
- If the deleted task is active, select another task in the same workspace when one exists.
- If the deleted task was the last task in that workspace, stay in that workspace and show an empty Tasks pane.

## PTY Lifecycle

Each task owns one PTY session, and that PTY is owned by the supervisor, not by an attached TUI client.

PTY key:

```text
task_id
```

PTY working directory:

```text
workspace path
```

Rules:

- Starting a task launches its configured command in its workspace.
- Switching tasks changes which PTY is rendered in `Task Live Preview` or `Task Console`.
- Task-focus input is forwarded to task PTYs as raw bytes, except for the configured drawer key that returns to the dashboard. For Codex agent tasks, terminal-generated C-c while Codex reports active work is routed through Codex's interrupt path.
- Configured command task progress is derived from submitted input and the PTY foreground process group, without injecting shell prompt hooks.
- Configured command task input must behave like the same shell outside Weft. Enhanced keyboard protocol sequences for ordinary typing and readline controls are decoded before forwarding, so keys such as `C-u` clear the current shell line instead of printing CSI-u bytes.
- Modifier-only enhanced keyboard events, such as bare Shift or Ctrl press reports, are ignored and must never be forwarded as literal CSI-u text.
- Command-K in a configured command task clears Weft's captured terminal screen and asks the shell to redraw at the top of the console.
- Forwarded task input preserves key order, including Esc, Alt/Meta prefixes, rapid typed text, and bracketed paste.
- Moving a task between groups does not affect its PTY.
- Top-level tasks have no group.
- Removing a workspace stops every PTY for tasks in that workspace.
- A task receives input only in Task Focus State.
- The active task PTY width matches the visible terminal content width inside the frame and the current pane padding, so terminal wrapping aligns with what the user sees.
- Configured command task screen buffers are top-aligned when the console grows so a newly opened shell prompt stays at the top instead of being pushed down by blank rows.
- The active task PTY height matches the visible content rows inside the frame.
- Cached PTY screen resizes preserve bottom rows first, and terminal top/bottom scrolling regions are honored so terminal footers and composers do not disappear behind the frame.
- Cached PTY screens preserve cursor visibility and DECSCUSR cursor shape requests so Vim-style block/bar/underline cursor changes render in the frame.
- Cached PTY screens model alternate-screen buffers separately from the normal task buffer. When a full-screen or pager view exits alternate screen, the prior normal screen and scrollback are restored without requiring the task to redraw after a dashboard focus toggle.
- Closing the TUI client does not stop PTYs.
- Restarting the supervisor stops PTYs unless a future implementation supports explicit PTY handoff.
- The active TUI client sends terminal size changes to the supervisor, and the supervisor resizes the active task PTY.
- When no client is attached, PTYs keep running at their most recent size.

## Command Semantics

`weft`:

- start the supervisor when it is not running
- attach an interactive TUI unless `--no-attach` is provided
- when `--clear` is provided, stop the supervisor, delete runtime state without a separate confirmation prompt, then start fresh
- do not require tmux

Global `--clear`:

- may be provided before or after any non-internal command, for example `weft --clear doctor keys` or `weft doctor keys --clear`
- stops the supervisor and deletes runtime state without a separate confirmation prompt before running the requested command
- creates a runtime backup before stopping or deleting state
- is ignored for help, version, the internal supervisor command, `weft clear`, `weft task notes ...`, and `weft skill install`

`weft close`:

- closes the current interactive client when run inside a client
- asks the supervisor to detach the active client when run from another shell
- does not stop the supervisor or task PTYs

`weft close --kill`:

- asks for confirmation when any task PTY is running
- creates a runtime backup before shutdown
- stops all task PTYs
- stops the supervisor
- preserves config and state

`weft refresh`:

- requests a fresh snapshot and repaint for the active client
- does not restart the supervisor
- does not clear state

`weft version`:

- prints the local CLI version
- is the only supported version-reporting CLI surface; `weft --version` is unsupported
- checks the current runtime socket without starting the supervisor or creating runtime config/state
- when a supervisor is running, prints the supervisor version, supervisor protocol version, upgrade status, and the active main dashboard client version
- reports the supervisor as not running and the main dashboard as not attached when no supervisor responds

`weft clear`:

- remains destructive
- stops the supervisor and all task PTYs
- creates a runtime backup before deletion
- deletes Weft runtime state after explicit confirmation

`weft backup create [--output <dir>] [--reason <text>]`:

- writes a backup of config, state, metadata, and available logs
- defaults to the current runtime's `backups/` directory
- must not copy sockets, locks, pid files, or live PTYs

`weft backup list`:

- lists backups in the current runtime's default backup directory

`weft backup restore <id-or-path> [--yes]`:

- resolves ids from the current runtime backup directory, or accepts a backup path
- creates a pre-restore backup before replacing current config and state
- restores config, state, and task notes only
- removes current config, state, or task notes when the selected backup did not contain it
- requires confirmation before stopping a running supervisor unless `--yes` is provided

`weft new [--type <id>] [title]`:

- creates a task in the selected workspace
- uses `default_task_type` when `--type` is omitted
- rejects unknown task type IDs
- applies an explicit title when supplied, otherwise copies the selected task type's `title_template`

`weft task notes set [--task <id>] <text...>`:

- persists a concise one-line note for a Codex task when `task_context.enabled = true`
- reads piped stdin when no text arguments are provided
- rejects empty terminal stdin, empty notes, multi-line short notes, and short notes larger than 512 bytes
- defaults to `WEFT_TASK_ID` when present, otherwise to the supervisor's active task
- rejects configured shell tasks because task notes are a Codex-task feature

`weft task notes show [--task <id>] [--json]`:

- prints the short note for the selected Codex task
- prints the `task_context` response object as JSON when `--json` is provided

`weft task notes clear [--task <id>]`:

- removes the short note for the selected Codex task
- also runs when there are no stored notes so automation can be idempotent

`weft task notes detail set [--task <id>] <text...>`:

- persists longer notes for a Codex task when `task_context.enabled = true`
- reads piped stdin when no text arguments are provided
- accepts newlines and rejects empty notes or content larger than 16 KiB
- defaults to `WEFT_TASK_ID` when present, otherwise to the supervisor's active task
- rejects configured shell tasks because task notes are a Codex-task feature

`weft task notes detail show [--task <id>] [--json]`:

- prints the longer notes for the selected Codex task
- prints the `task_context` response object as JSON when `--json` is provided

`weft task notes detail clear [--task <id>]`:

- removes longer notes for the selected Codex task
- also runs when there are no stored notes so automation can be idempotent

`weft skill install [--force]`:

- installs only the bundled `weft` Codex skill into `$CODEX_HOME/skills/weft`, defaulting to `~/.codex/skills/weft`
- does not edit Weft config
- fails when the skill already exists unless `--force` is provided

`weft doctor attention`:

- detects known terminal emulators from environment metadata
- for detected iTerm2 sessions on macOS, inspects the current/default profile's Notification Center alert setting, reports the preferences path and profile, offers to enable the setting after writing a plist backup, and requires explicit confirmation before mutating preferences
- sends an OSC 9 notification test when iTerm2 profile alerts are enabled and prints the remaining manual checks for iTerm2 Filter Alerts and macOS Notification settings
- if automatic terminal configuration fails, reports the failed step, preferences path, profile, wrapped command/output when available, and the manual fallback
- does not mutate terminal profiles or Weft configuration without explicit confirmation

`weft doctor memory`:

- reads the current runtime, state, supervisor pid file, and host process table
- reports current supervisor RSS, descendant task-process RSS, total Weft supervisor RSS on the machine, and other Weft supervisor count/RSS outside the current runtime
- warns when the pid file points to a missing supervisor process or when other Weft supervisors are present
- does not stop processes, delete runtime files, mutate state, or change configuration

`weft doctor keys`:

- interactively captures Backspace, Option+Backspace, and Ctrl+Backspace from the current terminal
- reports the raw bytes and interpreted key label for each key
- warns when Option+Backspace is indistinguishable from plain Backspace
- recommends configuring the terminal to send Option as Meta/Esc, including iTerm2 Left/Right Option Key set to Esc+ and the `1b 7f` custom mapping for Option+Backspace
- detects known terminal emulators from environment metadata
- for detected iTerm2 sessions on macOS, offers to set Left/Right Option Key to Esc+, add the Option+Backspace fallback key mapping to the current iTerm profile, write a plist backup first, and requires explicit confirmation
- when iTerm2 preferences already contain the full fix but the captured key still reports plain Backspace, explains that the current tab has not picked up the preference; for custom iTerm2 settings folders, recommends quitting and reopening iTerm2 because new tabs may keep using the in-memory profile
- if automatic terminal configuration fails, reports the failed step, preferences path, profile, wrapped command/output when available, and the manual fallback
- does not mutate terminal profiles or Weft configuration without explicit confirmation

`weft --help`, `weft help`, and `weft -h`:

- show the same Weft ASCII mark used by the empty task pane and no-task preview, with the woven mark scaled, a balanced diamond graph, a centered solid output arrowhead, and visible spacing before the block text
- show the active Weft version below the mark
- leave blank space above the mark and a small left inset before the mark
- advertise `weft` as the dashboard entry point
- group commands by common dashboard use, task organization, runtime, and configuration
- describe destructive commands explicitly
- do not include the title-template reference section

## Rendering Requirements

The UI should remain usable in small terminals.

Minimum behavior:

- The Workspaces pane has a fixed 60-column width when it is rendered beside the Tasks pane.
- At 116 columns and wider, show Workspaces, Tasks, and `Task Live Preview` panes together.
- At medium widths where a fixed Workspaces pane and useful `Task Live Preview` cannot both fit, keep Workspaces and Tasks visible and hide `Task Live Preview`.
- When all three panes fit, give the Tasks pane up to its preferred 54-column frame while preserving at least the minimum useful `Task Live Preview` width.
- If navigation cannot fit, fall back to a single navigation pane that switches between workspaces and tasks.
- Task Focus State must always give the active task the full available terminal.

Visual style:

- Terminal-native borders.
- Dense layouts.
- Minimal color.
- Use bordered cards only for the Workspaces pane.
- No table layout.
- No fixed status tags on task rows.

## Migration

Unsupported old state shapes are not migrated or archived. If `state.json` is not strict v6 current shape, including malformed references, missing titles, missing `type_id`, or task types not defined by active config, Weft returns a clear error that tells the user to run `weft clear` when a reset is acceptable.

## Configuration

Default config:

```toml
default_task_type = "codex"
title_hook_command = ""
title_hook_timeout_seconds = 10

[terminal_attention]
enabled = false
request_attention = "once"

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

[key_bindings]
drawer = "C-b"
focus_left = "Left"
focus_right = "Right"
select_prev = "k"
select_next = "j"
open = "Enter"
new_workspace = "w"
new_group = "g"
new_task = "n"
move_task = "m"
edit = "e"
delete = "Backspace"
repaint = "C-]"
help = "?"
quit = "C-c"
```

`key_bindings.delete = "d"` is not a valid current config value.

`key_bindings.repaint` configures the current Task Tools key. Inside Task Tools, `r` runs the repaint command.

Only the current config keys are emitted by default. Unknown config keys are rejected generically instead of being mapped silently or returning alias-specific repair advice.

## Testing Requirements

Unit tests:

- minute variations and pure logic details that do not require a live dashboard
- strict state parse rejection and reset guidance for unsupported state shapes
- supervisor startup, singleton locking, and shutdown decisions
- protocol field parsing and structured error formatting
- validation branches for workspace, group, task, task type, and config inputs
- title template rendering variants
- layout width calculations and rendering breakpoints
- prompt editing keystroke variants
- deterministic command construction and state helpers
- runtime guard decisions for source/release builds, explicit `WEFT_ROOT` or `WEFT_HOME`, and `WEFT_ALLOW_MAIN_RUNTIME=1`
- backup create/list/restore behavior, including missing state and pre-restore backup creation

Integration tests:

- all dashboard-supported, user-facing functionality at the journey level
- dashboard performance smoke checks with generous budgets for launch, prompt, task startup, refresh, and reattach latency
- launch with empty state
- launch without tmux installed or on `PATH`
- start supervisor with `weft --no-attach`
- attach, detach, and reattach while a task keeps running
- add workspace, group, task
- create Codex agent and configured shell command task types
- open task with `Enter`
- collapse or open a group with `Enter`
- verify nav panes collapse and the active task receives input
- verify Task Console and Task Live Preview mouse wheel scrollback, plus focused-console drag-copy selection
- reopen navigation and switch tasks
- remove workspace and verify tasks/PTYs close
- delete every group and task from a workspace and keep an empty Tasks pane
- persist and reload selected workspace/group/task state
- upgrade with no running tasks restarts the supervisor automatically
- upgrade with running Codex agent tasks preserves the old supervisor until tasks are idle/resumable and the user confirms `U`
- config drift with running tasks uses the same pending/ready `U` path, lists blocking tasks as workspace/task entries, and applies changed config only after the safe restart
- source/dev binary without `WEFT_ROOT` or `WEFT_HOME` fails before creating default runtime files
- automatic backups are created before `--clear`, `close --kill`, and idle upgrade auto-restart
- backup restore confirms before stopping a running supervisor unless `--yes` is provided
- source/dev runs from a Weft checkout derive their isolated runtime from the checkout cwd under `.weft-runtime`, so `go -C <checkout> run ./cmd/weft --clear` does not need `WEFT_ROOT` or `WEFT_HOME`

Integration tests should use temporary `WEFT_ROOT`, or temporary `WEFT_HOME` and `WEFT_WORKSPACE` when distinct paths are required, plus a fake Codex agent task command. They should not require tmux.

Verification workflow:

```text
gofmt -w cmd internal tests
go test ./...
WEFT_RUN_INTEGRATION=1 go test ./...
go build ./cmd/weft
```
