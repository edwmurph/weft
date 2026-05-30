# Weft Product Specification

This is the living product and technical specification for Weft. Keep this file current as the product evolves so implementation agents can treat it as the anchor definition.

## Product Definition

Weft is one global terminal command center for managing Codex agents across multiple workspaces.

Weft is no longer one instance per workspace. One local Weft supervisor owns the global navigation state, the agent registry, and Codex PTYs. Terminal UI clients attach to that supervisor, render the command center, and can detach without stopping agents. Users can organize agents by workspace, optionally place agents into flat groups, then enter a selected Codex thread when they want to interact with it.

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

## Supervisor

The supervisor is a local background process, referred to internally as `weftd`.
It is started automatically by `weft` when needed and is scoped by
`WEFT_HOME`. There is at most one active supervisor per runtime directory.

The supervisor owns:

- config loading and migration
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
client when the user closes Weft. `weft --no-attach` and `weft start
--no-attach` ensure the supervisor is running and then return. `--clear` may
be combined with either start form to force a fresh runtime before launch.

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
- keep existing agents and PTYs running
- offer a restart action for the supervisor

When no agents are running, Weft may restart the supervisor automatically to
finish the upgrade. When any agent PTY is running, Weft must not restart the
supervisor without explicit confirmation because that can stop live Codex
terminals.

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

Weft stores runtime files globally under `~/.weft` by default, or under
`WEFT_HOME` when set:

- `config.toml`
- `state.json`
- `weft.sock`
- `weftd.pid`
- `weftd.lock`
- `weftd.log`

`WEFT_WORKSPACE` overrides the launch directory used for attach-time workspace context. `WEFT_WORKDIR` remains a legacy compatibility alias.

## Primary Layout

The app has three logical panes.

## Workspaces Pane

The left pane lists configured workspaces as vertically stacked bordered cards.

When there are no configured workspaces, the pane shows centered help text telling
the user that there are no workspaces and to press the configured add-workspace key.

Each card renders:

- a title in the top border
- `total`, the number of all agents in that workspace
- `active`, the number of agents whose rendered/live status is `starting`, `running`, `working`, or `shipping`
- `needs attention`, computed as `total - active`

Do not render card-level `parked`, `stopped`, `quiet`, or `error` categories. Those agent states remain available to title templates and other agent-level surfaces, but the Workspaces pane summarizes them only through `needs attention`.

The default card title is the display path, for example `~/code/personal/weft`. A workspace can also have an optional manual title override. When the override is non-empty, the card uses that title instead of the path. Blank rename input clears the override and returns the card to the default path title.

Selection is indicated by the card border, not a full-row background. Use a stronger blue border when the Workspaces pane has focus. Use a subtler blue border when the selected workspace is active but focus is in the Agents pane.

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

- only the configured agent title string

Agent rows must not render fixed status tags. Status can appear only if the configured title template includes a status variable.

Group rows should be visually distinct from agent rows. Use the chevron/collapse marker, count, stronger color or weight, and extra vertical space before group sections. Agent rows should use a lighter marker and indentation when nested under a group.

## Codex Pane

The main pane shows either:

- a centered empty message when no agent is open, with a subtle Weft wordmark above it when space allows
- the selected Codex thread when an agent is open

When navigation is open, the Workspaces and Agents panes push the Console pane to the right. When the user presses `Enter` on an agent, navigation slides away left, the Console pane expands to the full terminal, and focus moves to Codex.

Codex can only receive input when the Console pane is focused and maximized. When navigation is open, keyboard input controls Weft navigation and organization, not the Codex PTY.

## Navigation States

Weft has two primary UI states.

## Command Center State

Navigation panes are open.

- Workspaces pane is visible.
- Agents pane is visible.
- Console pane is visible but not focused.
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

Autocomplete menus open directly under the input, use a bounded visible row count, and scroll to keep the highlighted option visible. Choosing an autocomplete option closes the menu and leaves the form in a normal submit state.

## Codex Focus State

Console pane is maximized.

- Workspaces and Agents panes are hidden offscreen to the left.
- Console pane fills the terminal.
- Weft keeps the framed Console pane visible while Codex is focused.
- The attached client enables enhanced terminal keyboard reporting and forwards
  supported keyboard escape sequences into the active Codex PTY.
- Codex-owned terminal behavior, including multiline shortcuts such as
  Shift+Enter in supporting terminals, is preserved inside the framed pane.
- C-c is not intercepted by Weft while Codex has focus.
- User exits back to command center with the configured drawer/navigation key.

## Initial Keybindings

These are product-level defaults and may map to existing config structures during implementation.

```text
Enter   Open selected agent and maximize Codex
C-b     Toggle command center navigation
Left/Right Move focus between workspaces and agents panes
j/k     Move selection within the focused navigation pane
w       Add workspace
g       Create group in selected workspace
n       Create agent in the current group when the cursor is on a group or grouped agent; otherwise create a top-level agent
m       Move selected agent to another group in the same workspace, or clear its group
r       Rename selected workspace title, group, or agent title
d       Delete/remove selected item
?       Help
C-c     Quit Weft from command center focus
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
error
```

The exact derivation of `ready`, `running`, and other live states can reuse the current Codex title/status detection and can evolve independently of the UI layout.

## Title Templates

Agent rows render a configured title string. Title templates are a global default only. They are not per-workspace, per-group, or per-agent in the first implementation.

An agent can still have its own base title. The global template controls how that title is rendered.

New agents default their base title to `{codex}` so they inherit the live Codex
title until renamed.

Default template:

```text
{title}
```

Supported variables:

```text
{title}    user-configured agent title
{auto}     generated title from the first submitted message
{codex}    live Codex title
{status}   derived/live agent status
{workspace}  display workspace path
{workdir}    legacy alias for {workspace}
{group}   flat group name
```

Example global templates:

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
`version`, `event`, `agent_id`, `workspace`, legacy `workdir`, `group`, `status`, `title`,
`title_template`, `codex_title`, and `first_message`.

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

The state model should move from flat tabs grouped by columns to global workspaces, flat groups, and agents.

The persisted JSON shape currently keeps historical `workdirs`, `workdir_id`, and related Go names for compatibility. Product UI, docs, commands, prompts, and title variables should call these objects workspaces.

Current compatibility shape:

```go
type State struct {
    Version          int       `json:"version"`
    ActiveAgentID    string    `json:"active_agent_id,omitempty"`
    SelectedWorkdirID string   `json:"selected_workdir_id,omitempty"`
    SelectedGroupID  string    `json:"selected_group_id,omitempty"`
    Focus            Focus     `json:"focus"`
    NavOpen          bool      `json:"nav_open"`
    Workdirs         []Workdir `json:"workdirs"`
    Groups           []Group   `json:"groups"`
    Agents           []Agent   `json:"agents"`
    CollapsedGroupIDs []string `json:"collapsed_group_ids,omitempty"`
}

type Workdir struct {
    ID        string `json:"id"`
    Path      string `json:"path"`
    Title     string `json:"title,omitempty"`
    CreatedAt string `json:"created_at"`
    UpdatedAt string `json:"updated_at"`
}

type Group struct {
    ID        string `json:"id"`
    WorkdirID string `json:"workdir_id"`
    Name      string `json:"name"`
    CreatedAt string `json:"created_at"`
    UpdatedAt string `json:"updated_at"`
}

type Agent struct {
    ID         string      `json:"id"`
    WorkdirID  string      `json:"workdir_id"`
    GroupID    string      `json:"group_id"`
    Title      string      `json:"title"`
    AutoTitle  string      `json:"auto_title,omitempty"`
    AutoTitleAttempted bool `json:"auto_title_attempted,omitempty"`
    AutoTitleError string `json:"auto_title_error,omitempty"`
    CodexTitle string      `json:"codex_title,omitempty"`
    Status     AgentStatus `json:"status"`
    CreatedAt  string      `json:"created_at"`
    UpdatedAt  string      `json:"updated_at"`
}
```

The implementation may keep legacy `folder` JSON field names while migrating existing users, but product UI, docs, commands, and prompts should call these objects groups.

Runtime-only details such as PID, PTY handles, socket clients, terminal size,
and screen cache must not be persisted in `state.json`. They belong to the
supervisor process and can be reconstructed from state and live PTYs.

The global title template belongs in config, not per agent:

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
- The persisted focus value may remain `workdirs` for compatibility; IPC should accept both `workspaces` and legacy `workdirs`.
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
- When an interactive client opens from a launch directory that is not configured, Weft asks whether to add it before mutating state.
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
- If the cursor is on a group or grouped agent, create the agent in that group.
- Otherwise create a top-level agent with no group.
- Starts a Codex PTY with the agent workspace as the process working directory.
- Uses the configured global title template for display.

Rename agent:

- Updates the agent base title.
- Does not change the global title template.

Move agent:

- Moves the agent to another group in the same workspace.
- Can also clear the group, moving the agent back to a top-level row.
- Dashboard prompt autocompletes existing group names in that workspace after the user types a matching prefix.
- Does not restart the PTY.
- Cross-workspace moves are out of scope for the first implementation.

Close/delete agent:

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
- Switching agents changes which PTY is rendered in the Console pane.
- Alt-modified keys are forwarded to Codex agent PTYs with an ESC prefix so terminal Meta key bindings keep working in the embedded Codex instance.
- Forwarded Codex input preserves key order, including rapid typed or pasted text.
- Moving an agent between groups does not affect its PTY.
- Top-level agents have no group.
- Removing a workspace stops every PTY for agents in that workspace.
- Codex receives input only in Codex Focus State.
- The active Codex PTY width matches the visible Codex content width inside the
  frame and left padding, so terminal wrapping aligns with what the user sees.
- Closing the TUI client does not stop PTYs.
- Restarting the supervisor stops PTYs unless a future implementation supports explicit PTY handoff.
- The active TUI client sends terminal size changes to the supervisor, and the supervisor resizes the active Codex PTY.
- When no client is attached, PTYs keep running at their most recent size.

## Command Semantics

`weft` and `weft start`:

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
- is ignored for help, version, the internal supervisor command, and
  `weft clear`

`weft close`:

- closes the current interactive client when run inside a client
- asks the supervisor to detach the active client when run from another shell
- does not stop the supervisor or agent PTYs

`weft close --kill`:

- asks for confirmation when any agent PTY is running
- stops all agent PTYs
- stops the supervisor
- preserves config and state

`weft refresh`:

- requests a fresh snapshot and repaint for the active client
- does not restart the supervisor
- does not clear state

`weft clear`:

- remains destructive
- stops the supervisor and all agent PTYs
- deletes Weft runtime state after explicit confirmation

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

`weft sessions`:

- lists the current supervisor and attached client state
- must not shell out to tmux

`weft --help`, `weft help`, and `weft -h`:

- show the same Weft ASCII mark used by the empty Console pane
- leave blank space above the mark and a small left inset before the mark
- advertise `weft` as the dashboard entry point rather than `weft start`
- group commands by common dashboard use, agent organization, runtime, and
  configuration
- describe destructive commands explicitly
- do not include the title-template reference section

`weft delete-session`:

- is legacy compatibility only after the supervisor architecture lands
- should be removed or replaced with a supervisor-scoped command before the next major CLI cleanup

## Rendering Requirements

The UI should remain usable in small terminals.

Minimum behavior:

- The Workspaces pane has a fixed 64-column width when it is rendered beside the
  Agents pane.
- At 120 columns and wider, show Workspaces, Agents, and Console panes together.
- At medium widths where a fixed Workspaces pane and useful Console preview cannot
  both fit, keep Workspaces and Agents visible and hide the Console preview.
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

Existing state with flat tabs and columns should migrate as follows:

- Current runtime workspace becomes the first workspace.
- Each old column becomes one flat group name.
- Each old tab becomes one agent.
- Preserve tab ID as agent ID when safe.
- Preserve title, Codex title, status, created timestamp, and updated timestamp.
- Preserve active tab as active agent.
- Set selected workspace and selected group from the active agent when the active agent has a group.
- Default `NavOpen` to false if an active agent exists, otherwise true.

Unsupported or legacy state should be archived before writing migrated state.

The supervisor architecture migration should preserve `config.toml`,
`state.json`, workspaces, groups, agents, titles, generated titles, selected
objects, and focus state. It cannot adopt live Codex PTYs from the old
legacy tmux-pane runtime. If an old runtime is running during upgrade, Weft
should explain that saved metadata will migrate but live terminals must be
restarted.

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

Existing keybindings can be migrated where names overlap.

`tmux_session` is legacy configuration. The supervisor architecture must ignore
or migrate it out of generated config. New generated config should not include a
tmux setting.

## Testing Requirements

Unit tests:

- state migration from old tabs/columns
- supervisor startup, singleton locking, and shutdown
- client/supervisor protocol handshake
- version mismatch and upgrade restart decisions
- command routing from CLI/TUI client to supervisor
- workspace creation/removal
- workspace title override set/clear behavior
- group create/rename/delete validation
- agent move within a workspace
- title template rendering
- focus transitions
- layout width calculations
- Codex input blocked unless maximized and focused
- TUI client detach does not kill agent PTYs

Integration tests:

- launch with empty state
- launch without tmux installed or on `PATH`
- start supervisor with `weft --no-attach`
- attach, detach, and reattach while an agent keeps running
- add workspace, group, agent
- open agent with `Enter`
- collapse or open a group with `Enter`
- verify nav panes collapse and Codex receives input
- reopen navigation and switch agents
- remove workspace and verify agents/PTYs close
- delete every group and agent from a workspace and keep an empty Agents pane
- persist and reload selected workspace/group/agent state
- upgrade with no running agents restarts the supervisor automatically
- upgrade with running agents preserves the old supervisor and prompts before restart

Integration tests should use temporary `WEFT_HOME`, temporary `WEFT_WORKSPACE`,
and a fake `codex_command`. They should not require tmux.

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
