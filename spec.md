# Weft Product Specification

This is the living product and technical specification for Weft. Keep this file current as the product evolves so implementation agents can treat it as the anchor definition.

## Product Definition

Weft is one global terminal dashboard for managing Codex agents across multiple workspaces.

Weft is no longer one instance per workspace. One local Weft supervisor owns the global navigation state, the agent registry, and Codex PTYs. Terminal UI clients attach to that supervisor, render the dashboard, and can detach without stopping agents. Users can organize agents by workspace, optionally place agents into flat groups, then enter a selected Codex thread when they want to interact with it.

The core workflow is:

1. Open Weft.
2. Use the left navigation panes to choose a workspace and agent.
3. Press `Enter` to maximize and focus the selected console.
4. Interact with Codex only while the Codex thread is focused and maximized.
5. Reopen navigation to switch, organize, create, move, rename, or close agents.

## Design Principles

- Global first: one Weft should manage all configured workspaces.
- Codex first when active: once an agent is opened, Codex gets the whole terminal.
- Navigation is structural, not workflow-stage based.
- Workspace and group movement is manual.
- Group names are flat strings.
- Groups are optional; agents can live directly in a workspace without a group.
- Agent rows render configured text only; no fixed status pills beside each row.
- The terminal UI should stay dense, minimal, and close to the current iTerm-style Weft look.
- Supervisor-owned sessions: agent PTYs must outlive any single TUI client.
- Disposable clients: closing, upgrading, or restarting a TUI client must not clear state or stop agents.
- No tmux runtime dependency: tmux must not be required for normal launch, attach, detach, rendering, upgrades, or tests.
- Event-driven speed: avoid polling loops and shelling out for routine runtime state.
- Minimal dependencies: prefer the Go standard library and existing terminal/PTY dependencies before adding new packages.

## Runtime Architecture

Weft has two runtime roles in one shipped binary.

Weft also has a build channel. Release/Homebrew builds set
`version.BuildChannel` to `release`; source builds default to `source`. A
source build must fail closed before reading or mutating the default
`~/.weft` runtime unless it can infer a checkout-local runtime from the current
working directory, `WEFT_ROOT` or `WEFT_HOME` is set explicitly, or
`WEFT_ALLOW_MAIN_RUNTIME=1` is set for an intentional one-off. Help, version,
and `weft doctor keys` remain available without default runtime access.

## Supervisor

The supervisor is a local background process, referred to internally as `weftd`.
It is started automatically by `weft` when needed and is scoped by
`WEFT_HOME`, or by `$WEFT_ROOT/.weft` when only `WEFT_ROOT` is set. There is at
most one active supervisor per runtime directory.

The supervisor owns:

- config loading
- state loading, repair, mutation, and persistence
- the agent registry
- Codex PTY processes
- terminal screen state for each agent
- title hook execution
- local IPC over a Unix domain socket
- attached client coordination
- version and protocol negotiation

The supervisor must not listen on a network interface. Its socket lives inside
the Weft runtime directory with user-only permissions.

## Clients

The `weft` command is a CLI and TUI client. By default it ensures the
supervisor is running, attaches an interactive terminal UI, and exits only the
client when the user closes Weft. `weft --no-attach` ensures the supervisor is
running and then returns. `--clear` may be combined with launch to force a fresh
runtime before launch.

The interactive client owns only terminal rendering, local input collection,
and transient modal editing state. Product state changes are sent to the
supervisor as commands. The supervisor responds with snapshots and event
updates that the client renders.

Only one interactive TUI client owns foreground rendering and input at a time in
the first implementation. A second `weft` attach should take over cleanly and
cause the previous client to exit with a short message that another client
attached. Non-interactive CLI commands such as `weft status` can run
concurrently.

## IPC

Client and supervisor communication should use a small versioned protocol over
the local Unix socket. The protocol should support:

- handshake with binary version and protocol version
- command request and response
- state snapshot response
- event subscription for state, PTY screen, status, and shutdown events
- key/input delivery to the active Codex PTY
- terminal size updates from the active TUI client
- structured errors suitable for CLI output and TUI footer messages

The protocol does not need an external RPC framework. New dependencies should
be added only if the standard library becomes clearly insufficient.

## Process And Upgrade UX

Users should not need `weft clear` after upgrades.

When a newly installed `weft` client finds an older compatible supervisor:

- attach to it successfully
- show a concise upgrade banner in the TUI and `weft status`
- clearly distinguish a client-only reopen from a supervisor restart: if the
  supervisor is still older, reopening the dashboard alone is not enough to
  finish the runtime upgrade
- show a concise bottom-of-Workspaces-pane tip with the client version,
  supervisor version, and the `U` restart-when-idle action while the dashboard
  navigation is open
- keep existing agents and PTYs running
- offer a restart-when-idle action for the supervisor from the dashboard

When no agents are running, Weft may restart the supervisor automatically to
finish the upgrade after creating a runtime backup. When any agent PTY is
running, Weft must not restart the supervisor without explicit confirmation
because that can stop live Codex terminals.

The in-dashboard restart action must be safe by default. If live Codex
terminals are running, it queues the restart and waits until no live Codex
terminal remains before creating a pre-upgrade backup, stopping the supervisor,
and starting the upgraded supervisor. It must not kill live Codex terminals just
because the user queued the action. When restart-when-idle is queued, pressing
`U` opens a confirmation to cancel the queued restart without stopping agents.

If the supervisor protocol is incompatible with the client, the client should
explain the situation and offer the least destructive recovery path:

```text
Weft was upgraded, but the running supervisor is too old for this client.
Restarting the supervisor will stop running Codex terminals. Saved layout and
metadata will remain.
```

`weft clear` remains a destructive last-resort reset. It must not be presented
as the normal upgrade path.

## Runtime Files

Weft stores runtime files globally under `~/.weft` by default, under
`$WEFT_ROOT/.weft` when `WEFT_ROOT` is set, or under `WEFT_HOME` when set:

- `config.toml`
- `state.json`
- `weft.sock`
- `weftd.pid`
- `weftd.lock`
- `weftd.log`
- `backups/`

`WEFT_ROOT` sets both development/worktree paths from one value: runtime files
go in `$WEFT_ROOT/.weft`, and the launch workspace is `$WEFT_ROOT`.
When source-built Weft runs from a Weft source checkout or detached worktree
without `WEFT_ROOT` or `WEFT_HOME`, the current working directory is treated as
that root. This keeps `go -C /path/to/weft-or-worktree run ./cmd/weft ...`
isolated to `/path/to/weft-or-worktree/.weft`.
`WEFT_WORKSPACE` overrides only the launch directory used for attach-time
workspace context. `WEFT_HOME` overrides only the runtime directory.
Development and worktree runs should usually rely on checkout-local auto-rooting
or set `WEFT_ROOT` to the worktree path.
The installed release command owns the real default `~/.weft` runtime.

Runtime backups live under `backups/<id>/` by default. A backup includes
`metadata.json`, `config.toml` when present, `state.json` when present, and log
files when present. Backups must not include sockets, locks, pid files, or live
PTY/process state.

## Development Worktree Hygiene

Repository-local development worktrees live under the primary checkout's
`.worktrees/` directory and are created with `scripts/create-worktree.sh
<slug>`. They should use `WEFT_ROOT=<worktree>` for manual Weft launches so each
worktree keeps its own `.weft/` runtime, supervisor socket, pid file, state, and
logs.

`scripts/cleanup-worktrees.sh` is the destructive cleanup path for disposable
auxiliary worktrees. It targets only Git-registered worktrees under
`.worktrees/`, preserves the primary checkout and registered external
worktrees, stops each target's `WEFT_ROOT` supervisor when one is present,
removes the worktree, and prunes stale Git worktree metadata. The script shows a
plan and asks for confirmation by default; `--dry-run` previews the same plan
without changing anything, and `--yes` confirms the cleanup for unattended use.

## Primary Layout

The app has three logical panes.

## Workspaces Pane

The left pane lists configured workspaces as vertically stacked bordered cards.

When there are no configured workspaces, the pane shows centered help text telling
the user that there are no workspaces and to press the configured add-workspace key.

Stored workspaces remain selectable even when their path is missing, unreadable,
or no longer a directory. In that bad-state case the card shows a visible warning
line such as `path missing; press d to remove`, using the configured delete key,
so the user can navigate to the entry and remove it without resetting all state.

Each card renders:

- a title in the top border
- `total`, the number of all agents in that workspace
- `active`, the number of agents whose rendered/live status is `starting`, `running`, `working`, or `shipping`
- `needs attention`, computed as `total - active`

Do not render card-level `parked`, `stopped`, `killed`, `quiet`, or `error` categories. Those agent states remain available to title templates and other agent-level surfaces, but the Workspaces pane summarizes them only through `needs attention`.

The default card title is the display path, for example `~/code/personal/weft`. A workspace can also have an optional manual title override. When the override is non-empty, the card uses that title instead of the path. Blank rename input clears the override and returns the card to the default path title.

Selection is indicated by the card border, not a full-row background. Use a stronger blue border when the Workspaces pane has focus. Use a subtler blue border when the selected workspace is active but focus is in the Agents pane.

When a newly installed client is attached to an older compatible supervisor, the
bottom of the Workspaces pane shows a concise upgrade tip. While any Codex agent
is still active, the tip waits for idle/resumable agents, for example
`Upgrade pending: client 7.5.5, supervisor 7.4.0. Wait for 1 agent(s) to become idle.`
When all remaining agents are idle and have saved Codex session IDs, the tip
shows the action, for example
`Upgrade ready: client 7.5.5, supervisor 7.4.0. Press U to upgrade and resume 2 idle agent(s).`
The tip must not imply that reopening the dashboard is enough to finish the
upgrade, and it must not suggest destructive reset commands while live agents
can be resumed. The confirmation modal explains that Weft closes idle Codex
terminals, restarts the supervisor, and runs `codex resume <session-id>` for
each saved agent. It also warns that running commands and unsubmitted terminal
input are not preserved, so users should finish important work first.

Counts should use subtle colors:

- `total`: muted neutral
- `active`: blue
- `needs attention`: amber when nonzero, muted neutral when zero

Example:

```text
╭ ~/code/personal/trading-engine ─────────────────────────────╮
│  8 total        3 active        5 needs attention            │
╰──────────────────────────────────────────────────────────────╯
```

## Agents Pane

The middle pane shows agents for the selected workspace. It is always present so the Workspaces pane can stay purely scoped to workspaces.

When no workspace exists or no workspace is selected, the Agents pane shows centered help text telling the user to add a workspace first. It must not advertise creating an agent until a workspace exists. When a workspace is selected but has no agents or groups, the Agents pane shows centered help text saying there are no agents and to press the configured new-agent key.

Agents without a group render as top-level rows. User-created groups render as collapsible sections inside this pane, with their member agents indented underneath. Creating a group must not force existing top-level agents into a visible `Ungrouped`, `General`, or `Inbox` section.

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
- number of agents in the group

Each agent row renders:

- only the rendered agent title template

Agent rows must not render fixed status tags. Status can appear only if the agent title template includes a status variable.

Agent rows may use subtle row color and marker shape to make derived state easier to scan. Rows for active non-ready agents (`starting`, `running`, `working`, or `shipping`) replace the static bullet marker with the shared high-resolution Braille loading spinner frame. Rows whose Codex PTY has not produced visible content may also use the spinner until the agent is ready. Ready and other attention-needed rows use a stronger warm marker/color treatment, while errors use the error marker/color. These visuals are presentation only and must not add status text.

Group rows should be visually distinct from agent rows. Use the chevron/collapse marker, count, stronger color or weight, and extra vertical space before group sections. Agent rows should use a lighter marker and indentation when nested under a group.

When the Agents pane has more rendered rows than fit in the visible frame, moving the cursor must scroll the pane enough to keep the selected group or agent row visible.

`Shift+Up` and `Shift+Down` on a selected agent row move that agent one row up
or down within its current group. Top-level agents reorder only within the
top-level ungrouped area. Reordering never moves an agent into or out of a
group, and does not restart the agent PTY.

## Agent Live Preview And Console

The main agent pane has two modes:

- `Agent Live Preview` when command center navigation is open
- `Agent Console` when the selected Codex thread is focused and maximized

The pane shows either:

- a centered empty message when no agent is open, with a subtle Weft wordmark and version label above it when space allows
- the selected Codex thread when an agent is open

When navigation is open, the Workspaces and Agents panes push `Agent Live Preview` to the right. The preview is read-only: keyboard input controls Weft navigation and organization, not the Codex PTY. When an agent is active, the preview top border shows the selected agent title at the top right; preview content reserves one inner column on both the left and right, and clipped terminal lines use a subtle reserved right-edge marker before the right padding so the pane reads as a live cropped lens instead of a full interactive terminal.

When the user presses `Enter` on an agent, navigation slides away left, `Agent Console` expands to the full terminal, and focus moves to Codex.

Codex can only receive input when `Agent Console` is focused and maximized.

## Navigation States

Weft has two primary UI states.

## Dashboard State

Navigation panes are open.

- Workspaces pane is visible.
- Agents pane is visible.
- `Agent Live Preview` pane is visible but not focused when terminal width allows it.
- Codex PTY does not receive normal key input.
- User can create, delete, rename, move, and select objects.

## Dashboard Form UX

All in-dashboard text-entry forms use the same compact modal treatment:

- rounded, bordered input directly below the field label
- one compact status or validation line below the input or suggestion menu
- short state-specific footer actions, such as `Enter save`, `Enter choose`, `Esc close suggestions`, and `Esc cancel`
- `Enter` submits only when the current value is valid for that form; invalid required values keep the form open and show the validation status
- `Esc` closes an open autocomplete menu first; otherwise it cancels the form
- prompt inputs support Option/Alt word movement and deletion when the terminal sends Option as Meta/Esc

Autocomplete appears only when it has a useful known set:

- path autocomplete for the add-workspace path prompt
- group-name autocomplete for moving an agent to an existing group

Autocomplete matches case-insensitive substrings in the relevant path segment or group name, not only prefixes from the beginning of the value.

Autocomplete menus open directly under the input, use a bounded visible row count, and scroll to keep the highlighted option visible. Choosing an autocomplete option closes the menu and leaves the form in a normal submit state.

## Codex Focus State

`Agent Console` is maximized.

- Workspaces and Agents panes are hidden offscreen to the left.
- `Agent Console` fills the terminal.
- Weft keeps the framed `Agent Console` pane visible while Codex is focused.
- The attached client forwards raw terminal input bytes into the active Codex
  PTY without key-name reconstruction. The configured drawer key, `C-b` by
  default, is the only dashboard key sequence owned by Weft.
- Terminal-generated C-c belongs to Codex whenever `Agent Console` is focused
  and an active agent exists. While Codex reports active work, Weft delivers C-c
  through Codex's interrupt path so running side-thread work is interrupted
  without returning from or closing the side thread. Weft does not use C-c to
  quit from `Agent Console`, and the toolbar must not advertise C-c.
- Codex-owned terminal behavior, including Vim mode, Esc timing, bracketed
  paste, Alt/Meta prefixes, and modified-key shortcuts such as Shift+Enter and
  Shift+Tab in supporting terminals, is preserved inside the framed pane.
- The framed terminal renderer preserves Codex cursor visibility and cursor
  shape requests, including block, underline, and bar cursor modes used by Vim
  insert/normal state.
- Weft enables cell-level mouse tracking in the attached client. In focused
  `Agent Console`, trackpad or wheel scrolling anywhere in the console frame
  moves through Weft's captured Codex scrollback instead of forwarding mouse
  packets into the active Codex PTY. Left-button drag selection starts after
  Codex's shared visual margin, so the highlighted cells and copied clipboard
  text match without post-copy text rewriting. While the drag highlight is
  active, it preserves Codex's existing foreground and background colors under
  the selection overlay.
  The console border shows a short copy-confirmation toast.
  Mouse input outside the focused console remains a Weft dashboard concern and
  is not forwarded to Codex.
- If the active Codex PTY exits while `Agent Console` is focused, Weft returns
  to the dashboard `Agents` pane, keeps the agent selected, marks it killed,
  and makes the exited state visible in agent metadata.
- User exits back to dashboard with the configured drawer/navigation key.

## Initial Keybindings

These are product-level defaults and may map to existing config structures during implementation.

```text
Enter   Open selected agent and maximize Codex
C-b     Toggle dashboard navigation
Left/Right Move focus between workspaces and agents panes
j/k     Move selection within the focused navigation pane
w       Add workspace
g       Create group in selected workspace
n       Create a top-level agent with no group
m       Move selected agent to another group in the same workspace, or clear its group
Shift+Up/Down Reorder selected agent within its group or top-level area
r       Rename selected workspace title, group, or agent title
d       Delete/remove selected item
?       Help
C-c     Quit Weft from dashboard focus
```

Deletion behavior depends on selected item type and is defined below.

## Status

Agent status exists in the model. The Workspaces pane summarizes status only as `active` and `needs attention` counts per workspace. A separate top-level global status summary is deferred.

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

The exact derivation of `ready`, `running`, and other live states can reuse the current Codex title/status detection and can evolve independently of the UI layout. When `{status}` is rendered from the live Codex terminal title, it preserves the exact casing of the matched Codex status token, such as `Ready` or `Working`; fallback lifecycle statuses remain the lowercase model values above.

## Title Templates

Agent rows render each agent's stored title template. The global title template is the default copied into new agents.

An agent title can include template variables. Renaming an agent edits that agent's stored template and does not change the global default.

New agents copy the configured global title template into their own title.

Default global template:

```text
{status} {auto}
```

Supported variables:

```text
{title}    user-configured agent title
{auto}     generated title from the first submitted message
{codex}    live Codex title
{status}   derived/live agent status, preserving Codex casing when live
{workspace}  display workspace path
{group}   flat group name
```

Example agent/default templates:

```text
{title}
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
{codex}  -> ...
{status} -> unknown
```

Generated titles are computed for every agent when `title_hook_command` is
configured. The first non-empty message submitted to the Codex PTY runs the
hook from the agent workspace, sends JSON on stdin, and stores the first non-empty
stdout line as the agent's generated title. The hook payload includes
`version`, `event`, `agent_id`, `workspace`, `group`, `status`, `title`, the
agent `title_template`, `codex_title`, and `first_message`.

Weft must not encode provider-specific clients, model names, API keys, or HTTP
contracts into the runtime. The title hook is just a shell command. If the hook
times out, exits nonzero, is missing, or writes no title, `{auto}` renders as
`auto title failed` and Weft does not retry for that agent.

When `{auto}` is added in the rename pane, the UI should make hook readiness
obvious. If `title_hook_command` is missing, show a configuration error. If the
title is already generated, explain that it is ready. Otherwise, explain that
auto title generation will run after the first submitted message. Hook failures
should be saved on the agent and shown in the footer and rename pane.

## Data Model

The state model uses global workspaces, flat groups, and agents.

Current persisted shape:

```go
type State struct {
    Version             int         `json:"version"`
    ActiveAgentID       string      `json:"active_agent_id,omitempty"`
    SelectedWorkspaceID string      `json:"selected_workspace_id,omitempty"`
    SelectedGroupID     string      `json:"selected_group_id,omitempty"`
    Focus               Focus       `json:"focus"`
    NavOpen             bool        `json:"nav_open"`
    Workspaces          []Workspace `json:"workspaces"`
    Groups              []Group     `json:"groups"`
    Agents           []Agent   `json:"agents"`
    CollapsedGroupIDs []string `json:"collapsed_group_ids,omitempty"`
}

type Workspace struct {
    ID        string `json:"id"`
    Path      string `json:"path"`
    Title     string `json:"title,omitempty"`
    CreatedAt string `json:"created_at"`
    UpdatedAt string `json:"updated_at"`
}

type Group struct {
    ID        string `json:"id"`
    WorkspaceID string `json:"workspace_id"`
    Path      string `json:"path"`
    CreatedAt string `json:"created_at"`
    UpdatedAt string `json:"updated_at"`
}

type Agent struct {
    ID         string      `json:"id"`
    WorkspaceID string    `json:"workspace_id"`
    GroupID    string      `json:"group_id"`
    Title      string      `json:"title"`
    AutoTitle  string      `json:"auto_title,omitempty"`
    AutoTitleAttempted bool `json:"auto_title_attempted,omitempty"`
    AutoTitleError string `json:"auto_title_error,omitempty"`
    CodexTitle string      `json:"codex_title,omitempty"`
    CodexSessionID string  `json:"codex_session_id,omitempty"`
    Status     AgentStatus `json:"status"`
    CreatedAt  string      `json:"created_at"`
    UpdatedAt  string      `json:"updated_at"`
}
```

Runtime-only details such as PID, PTY handles, socket clients, terminal size,
and screen cache must not be persisted in `state.json`. They belong to the
supervisor process and can be reconstructed from state and live PTYs.

The global title template belongs in config and is copied into new agents:

```toml
title_template = "{status} {auto}"
title_hook_command = ""
title_hook_timeout_seconds = 10
```

## Focus Model

Focus values:

```text
workspaces
agents
codex
```

Rules:

- `workspaces` and `agents` focus are valid only while navigation is open.
- `codex` focus is valid only while Codex is maximized.
- Opening an agent with `Enter` sets focus to `codex` and closes navigation.
- Reopening navigation moves focus back to the last focused navigation pane.
- Codex PTY input is blocked unless focus is `codex`.

## CRUD Semantics

## Workspace CRUD

Add workspace:

- User provides an existing filesystem path.
- Weft must not auto-add the launch directory during state repair or first supervisor start.
- When an interactive client opens from a launch directory that is already configured, Weft selects that workspace automatically.
- Launch-directory selection happens only when the interactive client attaches. It must not run on every snapshot, navigation, or delete request, because that would keep snapping selection back to the launch workspace and prevent removing stale workspace entries.
- When an interactive client opens from a launch directory that is not configured, Weft asks whether to add it before mutating state. `Enter` confirms; `Esc` declines.
- Dashboard prompt opens with the selected workspace's parent directory prefilled.
- Dashboard prompt uses the shared bordered form input.
- Autocomplete opens directly below the input when the user types or presses `Down`.
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
- Close all running agents in that workspace.
- Stop their PTYs.
- Remove associated groups and agents from state.
- Do not delete filesystem contents.
- Confirm before removal if any agent is running, ready, or shipping.

Rename workspace title:

- Workspaces are still identified by path and stable ID.
- Pressing `r` while focused on the Workspaces pane opens a workspace-title prompt.
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
- Keeps all agents in that group.
- Must remain unique within the workspace.

Delete group:

- Allowed only when empty.
- If non-empty, prompt the user to move agents first.
- Deleting the last group in a workspace is allowed.

## Agent CRUD

Create agent:

- Requires an existing selected workspace.
- Creates an agent in the selected workspace.
- Always creates a top-level agent with no group, even when the cursor is on a group or grouped agent.
- Starts a Codex PTY with the agent workspace as the process working directory.
- Copies the configured global title template into the agent title.

Rename agent:

- Opens with the stored agent title template.
- Updates the agent title template.
- Does not change the global default title template.

Move agent:

- Moves the agent to another group in the same workspace.
- Can also clear the group, moving the agent back to a top-level row.
- Dashboard prompt autocompletes existing group names in that workspace after the user types any matching substring.
- Does not restart the PTY.
- Cross-workspace moves are out of scope for the first implementation.

Reorder agent:

- `Shift+Up` and `Shift+Down` reorder the selected agent within its current
  group, or within the top-level ungrouped area when the agent has no group.
- Reordering never crosses group boundaries, never changes the workspace, and
  does not restart the PTY.

Close/delete agent:

- Confirmation explains that deleting stops the Codex terminal before removing
  the agent from Weft.
- Stops the PTY if running.
- Removes the agent from state.
- If the deleted agent is active, select another agent in the same workspace when one exists.
- If the deleted agent was the last agent in that workspace, stay in that workspace and show an empty Agents pane.

## PTY Lifecycle

Each agent owns one Codex PTY session, and that PTY is owned by the supervisor,
not by an attached TUI client.

PTY key:

```text
agent_id
```

PTY working directory:

```text
workspace path
```

Rules:

- Starting an agent launches Codex in its workspace.
- Switching agents changes which PTY is rendered in `Agent Live Preview` or `Agent Console`.
- Codex-focus input is forwarded to Codex agent PTYs as raw bytes, except for
  the configured drawer key that returns to the dashboard and terminal-generated
  C-c while Codex reports active work, which is routed through Codex's interrupt
  path.
- Forwarded Codex input preserves key order, including Esc, Alt/Meta prefixes,
  rapid typed text, and bracketed paste.
- Moving an agent between groups does not affect its PTY.
- Top-level agents have no group.
- Removing a workspace stops every PTY for agents in that workspace.
- Codex receives input only in Codex Focus State.
- The active Codex PTY width matches the visible Codex content width inside the
  frame and the current pane padding, so terminal wrapping aligns with what the
  user sees.
- The active Codex PTY height matches the visible content rows inside the frame.
- Cached PTY screen resizes preserve bottom rows first, and terminal
  top/bottom scrolling regions are honored so Codex chat footers and composers
  do not disappear behind the frame.
- Cached PTY screens preserve cursor visibility and DECSCUSR cursor shape
  requests so Vim-style block/bar/underline cursor changes render in the frame.
- Closing the TUI client does not stop PTYs.
- Restarting the supervisor stops PTYs unless a future implementation supports explicit PTY handoff.
- The active TUI client sends terminal size changes to the supervisor, and the supervisor resizes the active Codex PTY.
- When no client is attached, PTYs keep running at their most recent size.

## Command Semantics

`weft`:

- start the supervisor when it is not running
- attach an interactive TUI unless `--no-attach` is provided
- when `--clear` is provided, stop the supervisor, delete runtime state without
  a separate confirmation prompt, then start fresh
- do not require tmux

Global `--clear`:

- may be provided before or after any non-internal command, for example
  `weft --clear doctor keys` or `weft doctor keys --clear`
- stops the supervisor and deletes runtime state without a separate confirmation
  prompt before running the requested command
- creates a runtime backup before stopping or deleting state
- is ignored for help, version, the internal supervisor command, and
  `weft clear`

`weft close`:

- closes the current interactive client when run inside a client
- asks the supervisor to detach the active client when run from another shell
- does not stop the supervisor or agent PTYs

`weft close --kill`:

- asks for confirmation when any agent PTY is running
- creates a runtime backup before shutdown
- stops all agent PTYs
- stops the supervisor
- preserves config and state

`weft refresh`:

- requests a fresh snapshot and repaint for the active client
- does not restart the supervisor
- does not clear state

`weft version`:

- prints the local CLI version
- checks the current runtime socket without starting the supervisor or creating
  runtime config/state
- when a supervisor is running, prints the supervisor version, supervisor
  protocol version, upgrade status, and the active main dashboard client version
- reports the supervisor as not running and the main dashboard as not attached
  when no supervisor responds

`weft clear`:

- remains destructive
- stops the supervisor and all agent PTYs
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
- restores config and state only
- removes current config or state when the selected backup did not contain it
- requires confirmation before stopping a running supervisor unless `--yes` is provided

`weft doctor keys`:

- interactively captures Backspace, Option+Backspace, and Ctrl+Backspace from the current terminal
- reports the raw bytes and interpreted key label for each key
- warns when Option+Backspace is indistinguishable from plain Backspace
- recommends configuring the terminal to send Option as Meta/Esc, including iTerm2 Left/Right Option Key set to Esc+ and the `1b 7f` custom mapping for Option+Backspace
- detects known terminal emulators from environment metadata
- for detected iTerm2 sessions on macOS, offers to set Left/Right Option Key to Esc+, add the Option+Backspace fallback key mapping to the current iTerm profile, remove obsolete mappings written by earlier Weft attempts, write a plist backup first, and requires explicit confirmation
- when iTerm2 preferences already contain the full fix but the captured key still reports plain Backspace, explains that the current tab has not picked up the preference; for custom iTerm2 settings folders, recommends quitting and reopening iTerm2 because new tabs may keep using the in-memory profile
- if automatic terminal configuration fails, reports the failed step, preferences path, profile, wrapped command/output when available, and the manual fallback
- does not mutate terminal profiles or Weft configuration without explicit confirmation

`weft --help`, `weft help`, and `weft -h`:

- show the same Weft ASCII mark used by the empty agent pane, with the woven mark scaled, rendered with normal-width line strokes, and mirrored around the block wordmark's center line
- show the active Weft version below the mark
- leave blank space above the mark and a small left inset before the mark
- advertise `weft` as the dashboard entry point
- group commands by common dashboard use, agent organization, runtime, and
  configuration
- describe destructive commands explicitly
- do not include the title-template reference section

## Rendering Requirements

The UI should remain usable in small terminals.

Minimum behavior:

- The Workspaces pane has a fixed 60-column width when it is rendered beside the
  Agents pane.
- At 116 columns and wider, show Workspaces, Agents, and `Agent Live Preview` panes together.
- At medium widths where a fixed Workspaces pane and useful `Agent Live Preview` cannot
  both fit, keep Workspaces and Agents visible and hide `Agent Live Preview`.
- If navigation cannot fit, fall back to a single navigation pane that switches between workspaces and agents.
- Codex Focus State must always give Codex the full available terminal.

Visual style:

- Terminal-native borders.
- Dense layouts.
- Minimal color.
- Use bordered cards only for the Workspaces pane.
- No table layout.
- No fixed status tags on agent rows.

## Migration

Legacy state shapes are not migrated or archived. If `state.json` is not strict
v4, or if it still uses old tab, column, workdir, folder, or tmux-pane fields,
Weft returns a clear error that tells the user to run `weft clear` to reset.
Valid v4 state is still repaired for missing IDs, selections, timestamps, and
defaults inside the current schema.

## Configuration

Initial config additions:

```toml
title_template = "{status} {auto}"
title_hook_command = ""
title_hook_timeout_seconds = 10

[key_bindings]
drawer = "C-b"
focus_left = "Left"
focus_right = "Right"
select_prev = "k"
select_next = "j"
open = "Enter"
new_workspace = "w"
new_group = "g"
new_agent = "n"
move_agent = "m"
rename = "r"
delete = "d"
help = "?"
quit = "C-c"
```

Only the current config keys are loaded. Unknown keys, including legacy keys
such as `tmux_session`,
`columns`, `new_workdir`, `new_folder`, `focus_toggle`, `close_weft`, `prev`,
`previous`, `new`, and `close`, are rejected.

## Testing Requirements

Unit tests:

- minute variations and pure logic details that do not require a live dashboard
- state reset guidance for old tabs/columns, workdir/folder, tmux-pane, and unknown v4 shapes
- supervisor startup, singleton locking, and shutdown decisions
- protocol field parsing and structured error formatting
- validation branches for workspace, group, agent, and config inputs
- title template rendering variants
- layout width calculations and rendering breakpoints
- prompt editing keystroke variants
- deterministic command construction and state helpers
- runtime guard decisions for source/release builds, explicit `WEFT_ROOT` or `WEFT_HOME`, and
  `WEFT_ALLOW_MAIN_RUNTIME=1`
- backup create/list/restore behavior, including missing state and pre-restore
  backup creation

Integration tests:

- all dashboard-supported, user-facing functionality at the journey level
- dashboard performance smoke checks with generous budgets for launch, prompt, agent startup, refresh, and reattach latency
- launch with empty state
- launch without tmux installed or on `PATH`
- start supervisor with `weft --no-attach`
- attach, detach, and reattach while an agent keeps running
- add workspace, group, agent
- open agent with `Enter`
- collapse or open a group with `Enter`
- verify nav panes collapse and Codex receives input
- verify focused Agent Console mouse wheel scrollback and drag-copy selection
- reopen navigation and switch agents
- remove workspace and verify agents/PTYs close
- delete every group and agent from a workspace and keep an empty Agents pane
- persist and reload selected workspace/group/agent state
- upgrade with no running agents restarts the supervisor automatically
- upgrade with running agents preserves the old supervisor and prompts before restart
- source/dev binary without `WEFT_ROOT` or `WEFT_HOME` fails before creating default runtime files
- automatic backups are created before `--clear`, `close --kill`, and idle
  upgrade auto-restart
- backup restore confirms before stopping a running supervisor unless `--yes`
  is provided
- source/dev runs from a Weft checkout derive their isolated runtime from the
  checkout cwd, so `go -C <checkout> run ./cmd/weft --clear` does not need
  `WEFT_ROOT`

Integration tests should use temporary `WEFT_ROOT`, or temporary `WEFT_HOME`
and `WEFT_WORKSPACE` when distinct paths are required, plus a fake
`codex_command`. They should not require tmux.

Verification workflow:

```text
gofmt -w cmd internal tests
go test ./...
WEFT_RUN_INTEGRATION=1 go test ./...
go build ./cmd/weft
```

## Out Of Scope For First Implementation

- Top-level global status summary
- Nested group names
- Per-group title templates
- Per-agent title templates
- CLI command for workspace title overrides
- Cross-workspace agent moves
- Emoji picker
- Automatic group classification
- Multi-select batch operations
- PTY handoff across supervisor binary exec
- Multiple simultaneous interactive TUI clients
- Remote network access to a Weft supervisor
