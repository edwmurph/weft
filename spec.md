# Codux Product Specification

This is the living product and technical specification for Codux. Keep this file current as the product evolves so implementation agents can treat it as the anchor definition.

## Product Definition

Codux is one global terminal command center for managing Codex agents across multiple workdirs.

Codux is no longer one instance per workdir. A single Codux process owns the global navigation state, the agent registry, and Codex PTYs. Users can organize agents by workdir, optionally place agents into flat groups, then enter a selected Codex thread when they want to interact with it.

The core workflow is:

1. Open Codux.
2. Use the left navigation panes to choose a workdir and agent.
3. Press `Enter` to maximize and focus the selected Codex thread.
4. Interact with Codex only while the Codex thread is focused and maximized.
5. Reopen navigation to switch, organize, create, move, rename, or close agents.

## Design Principles

- Global first: one Codux should manage all configured workdirs.
- Codex first when active: once an agent is opened, Codex gets the whole terminal.
- Navigation is structural, not workflow-stage based.
- Workdir and group movement is manual.
- Group names are flat strings.
- Groups are optional; agents can live directly in a workdir without a group.
- Agent rows render configured text only; no fixed status pills beside each row.
- The terminal UI should stay dense, minimal, and close to the current iTerm-style Codux look.

## Primary Layout

The app has three logical panes.

## Workdirs Pane

The left pane lists configured workdirs.

Each row renders:

- a workdir icon, default `📁` when the terminal supports it
- the display path, for example `~/code/personal/codux`
- the number of agents in that workdir

Rows should stay minimal. Do not render workdir aliases, status tags, extra metadata, or descriptions in the default view.

Example:

```text
📁 ~/code/personal/codux              8
📁 ~/code/personal/trading-engine     3
📁 ~/code/personal/configs            2
```

If Unicode workdir icons are unavailable or disabled, fall back to a plain text marker such as `[workdir]`.

## Agents Pane

The middle pane shows agents for the selected workdir. It is always present so the Workdirs pane can stay purely scoped to workdirs.

Agents without a group render as top-level rows. User-created groups render as collapsible sections inside this pane, with their member agents indented underneath. Creating a group must not force existing top-level agents into a visible `Ungrouped`, `General`, or `Inbox` section.

Group names are plain text. Emojis are inherently allowed because the group name is just user text. Codux does not need a separate emoji feature, picker, or icon system for groups in the first implementation.

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

- a centered empty message when no agent is open
- the selected Codex thread when an agent is open

When navigation is open, the workdirs and agents panes push the Codex pane to the right. When the user presses `Enter` on an agent, navigation slides away left, the Codex pane expands to the full terminal, and focus moves to Codex.

Codex can only receive input when the Codex pane is focused and maximized. When navigation is open, keyboard input controls Codux navigation and organization, not the Codex PTY.

## Navigation States

Codux has two primary UI states.

## Command Center State

Navigation panes are open.

- Workdirs pane is visible.
- Agents pane is visible.
- Codex pane is visible but not focused.
- Codex PTY does not receive normal key input.
- User can create, delete, rename, move, and select objects.

## Codex Focus State

Codex pane is maximized.

- Workdirs and agents panes are hidden offscreen to the left.
- Codex pane fills the terminal.
- Codex PTY receives normal key input.
- User exits back to command center with the configured drawer/navigation key.

## Initial Keybindings

These are product-level defaults and may map to existing config structures during implementation.

```text
Enter   Open selected agent and maximize Codex
C-b     Toggle command center navigation
Left/Right Move focus between workdirs and agents panes
j/k     Move selection within the focused navigation pane
w       Create workdir
g       Create group in selected workdir
n       Create agent in the current group when the cursor is on a group or grouped agent; otherwise create a top-level agent
m       Move selected agent to another group in the same workdir, or clear its group
r       Rename selected group or agent title
d       Delete/remove selected item
?       Help
C-c     Quit Codux
```

Deletion behavior depends on selected item type and is defined below.

## Status

Agent status exists in the model but global status counts are deferred for now.

Do not implement global `ready`, `running`, `sitting`, or `shipping` counts in the first pass. This can be added later as a top-level command center summary.

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

Agent rows render a configured title string. Title templates are a global default only. They are not per-workdir, per-group, or per-agent in the first implementation.

An agent can still have its own base title. The global template controls how that title is rendered.

Default template:

```text
{title}
```

Supported variables:

```text
{title}    user-configured agent title
{codex}    live Codex title
{status}   derived/live agent status
{workdir}  display workdir path
{group}   flat group name
```

Example global templates:

```text
{title}
{status} {title}
{group}: {title}
{workdir} / {group} / {title}
```

If a variable is unavailable, render a stable fallback rather than an empty broken string:

```text
{title}  -> ...
{codex}  -> ...
{status} -> unknown
```

## Data Model

The state model should move from flat tabs grouped by columns to global workdirs, flat groups, and agents.

Recommended normalized shape:

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
    CodexTitle string      `json:"codex_title,omitempty"`
    Status     AgentStatus `json:"status"`
    CreatedAt  string      `json:"created_at"`
    UpdatedAt  string      `json:"updated_at"`
}
```

The implementation may keep legacy `folder` JSON field names while migrating existing users, but product UI, docs, commands, and prompts should call these objects groups.

The global title template belongs in config, not per agent:

```toml
title_template = "{title}"
```

## Focus Model

Focus values:

```text
workdirs
agents
codex
```

Rules:

- `workdirs` and `agents` focus are valid only while navigation is open.
- `codex` focus is valid only while Codex is maximized.
- Opening an agent with `Enter` sets focus to `codex` and closes navigation.
- Reopening navigation moves focus back to the last focused navigation pane.
- Codex PTY input is blocked unless focus is `codex`.

## CRUD Semantics

## Workdir CRUD

Create workdir:

- User provides an existing filesystem path.
- Store the absolute path.
- Display path using `~` when possible.
- Do not create a default group.

Remove workdir:

- Remove the workdir from Codux state.
- Close all running agents in that workdir.
- Stop their PTYs.
- Remove associated groups and agents from state.
- Do not delete filesystem contents.
- Confirm before removal if any agent is running, ready, or shipping.

Rename workdir:

- Workdirs are identified and displayed by path.
- Rename is out of scope for the first implementation.
- Future alias support can be added without changing the core model.

## Group CRUD

Create group:

- Creates a flat group name inside the selected workdir.
- Name must be unique within that workdir.
- Name may include emoji and normal Unicode text.
- Name must not contain `/` in the first implementation.

Rename group:

- Updates the flat group name.
- Keeps all agents in that group.
- Must remain unique within the workdir.

Delete group:

- Allowed only when empty.
- If non-empty, prompt the user to move agents first.
- Deleting the last group in a workdir is allowed.

## Agent CRUD

Create agent:

- Creates an agent in the selected workdir.
- If the cursor is on a group or grouped agent, create the agent in that group.
- Otherwise create a top-level agent with no group.
- Starts a Codex PTY with the agent workdir as the process working directory.
- Uses the configured global title template for display.

Rename agent:

- Updates the agent base title.
- Does not change the global title template.

Move agent:

- Moves the agent to another group in the same workdir.
- Can also clear the group, moving the agent back to a top-level row.
- Does not restart the PTY.
- Cross-workdir moves are out of scope for the first implementation.

Close/delete agent:

- Stops the PTY if running.
- Removes the agent from state.
- If the deleted agent is active, select another agent in the same workdir when one exists.
- If the deleted agent was the last agent in that workdir, stay in that workdir and show an empty Agent pane.

## PTY Lifecycle

Each agent owns one Codex PTY session.

PTY key:

```text
agent_id
```

PTY working directory:

```text
workdir.path
```

Rules:

- Starting an agent launches Codex in its workdir.
- Switching agents changes which PTY is rendered in the Codex pane.
- Moving an agent between groups does not affect its PTY.
- Top-level agents have no group.
- Removing a workdir stops every PTY for agents in that workdir.
- Codex receives input only in Codex Focus State.

## Rendering Requirements

The UI should remain usable in small terminals.

Minimum behavior:

- If width is constrained, shrink or hide the workdirs pane first.
- Preserve the agents pane enough to select an agent.
- If navigation cannot fit, fall back to a single navigation pane that switches between workdirs and agents.
- Codex Focus State must always give Codex the full available terminal.

Visual style:

- Terminal-native borders.
- Dense rows.
- Minimal color.
- No card-heavy dashboard.
- No table layout.
- No fixed status tags on agent rows.

## Migration

Existing state with flat tabs and columns should migrate as follows:

- Current runtime workdir becomes the first workdir.
- Each old column becomes one flat group name.
- Each old tab becomes one agent.
- Preserve tab ID as agent ID when safe.
- Preserve title, Codex title, status, created timestamp, and updated timestamp.
- Preserve active tab as active agent.
- Set selected workdir and selected group from the active agent when the active agent has a group.
- Default `NavOpen` to false if an active agent exists, otherwise true.

Unsupported or legacy state should be archived before writing migrated state.

## Configuration

Initial config additions:

```toml
title_template = "{title}"

[key_bindings]
drawer = "C-b"
focus_left = "Left"
focus_right = "Right"
select_prev = "k"
select_next = "j"
open = "Enter"
new_workdir = "w"
new_group = "g"
new_agent = "n"
move_agent = "m"
rename = "r"
delete = "d"
help = "?"
quit = "C-c"
```

Existing keybindings can be migrated where names overlap.

## Testing Requirements

Unit tests:

- state migration from old tabs/columns
- workdir creation/removal
- group create/rename/delete validation
- agent move within a workdir
- title template rendering
- focus transitions
- layout width calculations
- Codex input blocked unless maximized and focused

Integration tests:

- launch with empty state
- add workdir, group, agent
- open agent with `Enter`
- collapse or open a group with `Enter`
- verify nav panes collapse and Codex receives input
- reopen navigation and switch agents
- remove workdir and verify agents/PTYs close
- delete every group and agent from a workdir and keep an empty Agents pane
- persist and reload selected workdir/group/agent state

Verification workflow:

```text
gofmt -w cmd internal tests
go test ./...
CODUX_RUN_INTEGRATION=1 go test ./...
go build ./cmd/codux
```

## Out Of Scope For First Implementation

- Global status counts UI
- Nested group names
- Per-group title templates
- Per-agent title templates
- Workdir aliases
- Cross-workdir agent moves
- Emoji picker
- Automatic group classification
- Multi-select batch operations
