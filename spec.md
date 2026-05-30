# Codux Product Specification

This is the living product and technical specification for Codux. Keep this file current as the product evolves so implementation agents can treat it as the anchor definition.

## Product Definition

Codux is one global terminal command center for managing Codex agents across multiple workdirs.

Codux is no longer one instance per workdir. A single Codux process owns the global navigation state, the agent registry, and Codex PTYs. Users can organize agents by workdir and flat folders, then enter a selected Codex thread when they want to interact with it.

The core workflow is:

1. Open Codux.
2. Use the left navigation panes to choose a workdir, folder, and agent.
3. Press `Enter` to maximize and focus the selected Codex thread.
4. Interact with Codex only while the Codex thread is focused and maximized.
5. Reopen navigation to switch, organize, create, move, rename, or close agents.

## Design Principles

- Global first: one Codux should manage all configured workdirs.
- Codex first when active: once an agent is opened, Codex gets the whole terminal.
- Navigation is structural, not workflow-stage based.
- Workdir and folder movement is manual.
- Folder paths are flat strings.
- Agent rows render configured text only; no fixed status pills beside each row.
- The terminal UI should stay dense, minimal, and close to the current iTerm-style Codux look.

## Primary Layout

The app has three logical panes.

## Workdirs Pane

The left pane lists configured workdirs.

Each row renders:

- a folder icon, default `📁` when the terminal supports it
- the display path, for example `~/code/personal/codux`
- the number of agents in that workdir

Rows should stay minimal. Do not render workdir aliases, status tags, extra metadata, or descriptions in the default view.

Example:

```text
📁 ~/code/personal/codux              8
📁 ~/code/personal/trading-engine     3
📁 ~/code/personal/configs            2
```

If Unicode folder icons are unavailable or disabled, fall back to a plain text marker such as `[folder]`.

## Folders Pane

The middle pane shows folders for the selected workdir and the agents inside those folders.

Folder names are plain text. Emojis are inherently allowed because the folder path is just user text. Codux does not need a separate emoji feature, picker, or icon system for folders in the first implementation.

Folder paths are flat. Valid folder examples:

```text
dashboard
release
client-followups
🧪 ideas
```

Nested folder paths are out of scope for the first implementation. Treat strings containing `/` as invalid folder paths unless this spec is updated.

Each folder row renders:

- folder path text
- number of agents in the folder

Each agent row renders:

- only the configured agent title string

Agent rows must not render fixed status tags. Status can appear only if the configured title template includes a status variable.

## Codex Pane

The main pane shows either:

- a centered empty message when no agent is open
- the selected Codex thread when an agent is open

When navigation is open, the workdirs and folders panes push the Codex pane to the right. When the user presses `Enter` on an agent, navigation slides away left, the Codex pane expands to the full terminal, and focus moves to Codex.

Codex can only receive input when the Codex pane is focused and maximized. When navigation is open, keyboard input controls Codux navigation and organization, not the Codex PTY.

## Navigation States

Codux has two primary UI states.

## Command Center State

Navigation panes are open.

- Workdirs pane is visible.
- Folders pane is visible.
- Codex pane is visible but not focused.
- Codex PTY does not receive normal key input.
- User can create, delete, rename, move, and select objects.

## Codex Focus State

Codex pane is maximized.

- Workdirs and folders panes are hidden offscreen to the left.
- Codex pane fills the terminal.
- Codex PTY receives normal key input.
- User exits back to command center with the configured drawer/navigation key.

## Initial Keybindings

These are product-level defaults and may map to existing config structures during implementation.

```text
Enter   Open selected agent and maximize Codex
C-b     Toggle command center navigation
h/l     Move focus between workdirs and folders panes
j/k     Move selection within the focused navigation pane
w       Create workdir
f       Create folder in selected workdir
n       Create agent in selected folder
m       Move selected agent to another folder in the same workdir
r       Rename selected folder or agent title
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

Agent rows render a configured title string. Title templates are a global default only. They are not per-workdir, per-folder, or per-agent in the first implementation.

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
{folder}   flat folder path
```

Example global templates:

```text
{title}
{status} {title}
{folder}: {title}
{workdir} / {folder} / {title}
```

If a variable is unavailable, render a stable fallback rather than an empty broken string:

```text
{title}  -> ...
{codex}  -> ...
{status} -> unknown
```

## Data Model

The state model should move from flat tabs grouped by columns to global workdirs, flat folders, and agents.

Recommended normalized shape:

```go
type State struct {
    Version          int       `json:"version"`
    ActiveAgentID    string    `json:"active_agent_id,omitempty"`
    SelectedWorkdirID string   `json:"selected_workdir_id,omitempty"`
    SelectedFolderID string    `json:"selected_folder_id,omitempty"`
    Focus            Focus     `json:"focus"`
    NavOpen          bool      `json:"nav_open"`
    Workdirs         []Workdir `json:"workdirs"`
    Folders          []Folder  `json:"folders"`
    Agents           []Agent   `json:"agents"`
}

type Workdir struct {
    ID        string `json:"id"`
    Path      string `json:"path"`
    CreatedAt string `json:"created_at"`
    UpdatedAt string `json:"updated_at"`
}

type Folder struct {
    ID        string `json:"id"`
    WorkdirID string `json:"workdir_id"`
    Path      string `json:"path"`
    CreatedAt string `json:"created_at"`
    UpdatedAt string `json:"updated_at"`
}

type Agent struct {
    ID         string      `json:"id"`
    WorkdirID  string      `json:"workdir_id"`
    FolderID   string      `json:"folder_id"`
    Title      string      `json:"title"`
    CodexTitle string      `json:"codex_title,omitempty"`
    Status     AgentStatus `json:"status"`
    CreatedAt  string      `json:"created_at"`
    UpdatedAt  string      `json:"updated_at"`
}
```

The global title template belongs in config, not per agent:

```toml
title_template = "{title}"
```

## Focus Model

Focus values:

```text
workdirs
folders
codex
```

Rules:

- `workdirs` and `folders` focus are valid only while navigation is open.
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
- Create a default folder if the workdir has no folders.

Remove workdir:

- Remove the workdir from Codux state.
- Close all running agents in that workdir.
- Stop their PTYs.
- Remove associated folders and agents from state.
- Do not delete filesystem contents.
- Confirm before removal if any agent is running, ready, or shipping.

Rename workdir:

- Workdirs are identified and displayed by path.
- Rename is out of scope for the first implementation.
- Future alias support can be added without changing the core model.

## Folder CRUD

Create folder:

- Creates a flat folder path inside the selected workdir.
- Path must be unique within that workdir.
- Path may include emoji and normal Unicode text.
- Path must not contain `/` in the first implementation.

Rename folder:

- Updates the flat folder path.
- Keeps all agents in that folder.
- Must remain unique within the workdir.

Delete folder:

- Allowed only when empty.
- If non-empty, prompt the user to move agents first.

## Agent CRUD

Create agent:

- Creates an agent in the selected workdir and selected folder.
- Starts a Codex PTY with the agent workdir as the process working directory.
- Uses the configured global title template for display.

Rename agent:

- Updates the agent base title.
- Does not change the global title template.

Move agent:

- Moves the agent to another folder in the same workdir.
- Does not restart the PTY.
- Cross-workdir moves are out of scope for the first implementation.

Close/delete agent:

- Stops the PTY if running.
- Removes the agent from state.
- If the deleted agent is active, the Codex pane returns to empty state or selects the next available agent.

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
- Moving an agent between folders does not affect its PTY.
- Removing a workdir stops every PTY for agents in that workdir.
- Codex receives input only in Codex Focus State.

## Rendering Requirements

The UI should remain usable in small terminals.

Minimum behavior:

- If width is constrained, shrink or hide the workdirs pane first.
- Preserve the folders pane enough to select an agent.
- If navigation cannot fit, fall back to a single navigation pane that switches between workdirs and folders.
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
- Each old column becomes one flat folder path.
- Each old tab becomes one agent.
- Preserve tab ID as agent ID when safe.
- Preserve title, Codex title, status, created timestamp, and updated timestamp.
- Preserve active tab as active agent.
- Set selected workdir and selected folder from the active agent.
- Default `NavOpen` to false if an active agent exists, otherwise true.

Unsupported or legacy state should be archived before writing migrated state.

## Configuration

Initial config additions:

```toml
title_template = "{title}"

[key_bindings]
drawer = "C-b"
focus_left = "h"
focus_right = "l"
select_prev = "k"
select_next = "j"
open = "Enter"
new_workdir = "w"
new_folder = "f"
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
- folder create/rename/delete validation
- agent move within a workdir
- title template rendering
- focus transitions
- layout width calculations
- Codex input blocked unless maximized and focused

Integration tests:

- launch with empty state
- add workdir, folder, agent
- open agent with `Enter`
- verify nav panes collapse and Codex receives input
- reopen navigation and switch agents
- remove workdir and verify agents/PTYs close
- persist and reload selected workdir/folder/agent state

Verification workflow:

```text
gofmt -w cmd internal tests
go test ./...
CODUX_RUN_INTEGRATION=1 go test ./...
go build ./cmd/codux
```

## Out Of Scope For First Implementation

- Global status counts UI
- Nested folder paths
- Per-folder title templates
- Per-agent title templates
- Workdir aliases
- Cross-workdir agent moves
- Emoji picker
- Automatic folder classification
- Multi-select batch operations
