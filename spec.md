# Weft Product Specification

This is the living product and technical specification for Weft. Keep this file current as the product evolves so implementation agents can treat it as the anchor definition.

## Product Definition

Weft is one global terminal command center for managing Codex agents across multiple workdirs.

Weft is no longer one instance per workdir. A single Weft process owns the global navigation state, the agent registry, and Codex PTYs. Users can organize agents by workdir, optionally place agents into flat groups, then enter a selected Codex thread when they want to interact with it.

The core workflow is:

1. Open Weft.
2. Use the left navigation panes to choose a workdir and agent.
3. Press `Enter` to maximize and focus the selected Codex thread.
4. Interact with Codex only while the Codex thread is focused and maximized.
5. Reopen navigation to switch, organize, create, move, rename, or close agents.

## Design Principles

- Global first: one Weft should manage all configured workdirs.
- Codex first when active: once an agent is opened, Codex gets the whole terminal.
- Navigation is structural, not workflow-stage based.
- Workdir and group movement is manual.
- Group names are flat strings.
- Groups are optional; agents can live directly in a workdir without a group.
- Agent rows render configured text only; no fixed status pills beside each row.
- The terminal UI should stay dense, minimal, and close to the current iTerm-style Weft look.

## Primary Layout

The app has three logical panes.

## Workdirs Pane

The left pane lists configured workdirs as vertically stacked bordered cards.

Each card renders:

- a title in the top border
- `total`, the number of all agents in that workdir
- `active`, the number of agents whose rendered/live status is `starting`, `running`, `working`, or `shipping`
- `needs attention`, computed as `total - active`

Do not render card-level `parked`, `stopped`, `quiet`, or `error` categories. Those agent states remain available to title templates and other agent-level surfaces, but the Workdirs pane summarizes them only through `needs attention`.

The default card title is the display path, for example `~/code/personal/weft`. A workdir can also have an optional manual title override. When the override is non-empty, the card uses that title instead of the path. Blank rename input clears the override and returns the card to the default path title.

Selection is indicated by the card border, not a full-row background. Use a stronger blue border when the Workdirs pane has focus. Use a subtler blue border when the selected workdir is active but focus is in the Agents pane.

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

The middle pane shows agents for the selected workdir. It is always present so the Workdirs pane can stay purely scoped to workdirs.

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

When navigation is open, the workdirs and agents panes push the Codex pane to the right. When the user presses `Enter` on an agent, navigation slides away left, the Codex pane expands to the full terminal, and focus moves to Codex.

Codex can only receive input when the Codex pane is focused and maximized. When navigation is open, keyboard input controls Weft navigation and organization, not the Codex PTY.

## Navigation States

Weft has two primary UI states.

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
r       Rename selected workdir title, group, or agent title
d       Delete/remove selected item
?       Help
C-c     Quit Weft
```

Deletion behavior depends on selected item type and is defined below.

## Status

Agent status exists in the model. The Workdirs pane summarizes status only as `active` and `needs attention` counts per workdir. A separate top-level global status summary is deferred.

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
{workdir}  display workdir path
{group}   flat group name
```

Example global templates:

```text
{title}
{auto}
{status} {title}
{group}: {title}
{workdir} / {group} / {title}
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
hook from the agent workdir, sends JSON on stdin, and stores the first non-empty
stdout line as the agent's generated title. The hook payload includes
`version`, `event`, `agent_id`, `workdir`, `group`, `status`, `title`,
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

The global title template belongs in config, not per agent:

```toml
title_template = "{title}"
title_hook_command = ""
title_hook_timeout_seconds = 10
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

- Remove the workdir from Weft state.
- Close all running agents in that workdir.
- Stop their PTYs.
- Remove associated groups and agents from state.
- Do not delete filesystem contents.
- Confirm before removal if any agent is running, ready, or shipping.

Rename workdir title:

- Workdirs are still identified by path and stable ID.
- Pressing `r` while focused on the Workdirs pane opens a workdir-title prompt.
- Non-empty input stores the optional workdir title override.
- Blank input clears the override and returns display to the default path title.
- No CLI command is required for workdir title changes in the first implementation.

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
- Dense layouts.
- Minimal color.
- Use bordered cards only for the Workdirs pane.
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
title_hook_command = ""
title_hook_timeout_seconds = 10

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
- workdir title override set/clear behavior
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
WEFT_RUN_INTEGRATION=1 go test ./...
go build ./cmd/weft
```

## Out Of Scope For First Implementation

- Top-level global status summary
- Nested group names
- Per-group title templates
- Per-agent title templates
- CLI command for workdir title overrides
- Cross-workdir agent moves
- Emoji picker
- Automatic group classification
- Multi-select batch operations
