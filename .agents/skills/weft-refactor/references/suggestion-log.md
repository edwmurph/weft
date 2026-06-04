# Weft Refactor Suggestion Log

Use this log to preserve concrete refactor suggestions, decisions, and evidence across sessions. Keep entries short, append new sessions at the top, and update an entry's outcome when the user accepts, rejects, or defers a suggestion.

## 2026-06-04 - Repo-local skill discovery consolidation

- Request: Move the demo video skill and any other repo-local skills with the same discovery problem into the standard Codex-discovered `.agents/skills` convention.
- Suggestions: Consolidate `weft-demo-video`, `weft-refactor`, and existing repo-local skill guidance under `.agents/skills`, remove the repo-root `skills/` directory, and rewrite current instructions so future agents do not look in the undiscovered location.
- Outcome: Implemented in `.worktrees/skill-consolidation`; awaiting review.
- Evidence: `AGENTS.md`, `.agents/skills/weft-demo-video`, `.agents/skills/weft-refactor`, `.agents/skills/weft-cleanup`.
- Deferred: None.

## 2026-06-04 - Test duplicate cleanup

- Request: Do a safe refactor that reduces lines of code and makes Weft easier to manage, scale, and reason about.
- Suggestions: Keep runtime behavior unchanged and remove repeated test harness code by table-driving strict state rejection cases, client input router send/read setup, mouse selection-offset cases, and IPC `new` rejection cases.
- Outcome: Implemented in `.worktrees/safe-refactor-cleanup`; awaiting review.
- Evidence: `internal/state/state_test.go`, `internal/tui/client_input_test.go`, `internal/tui/mouse_test.go`, `internal/tui/model_test.go`; `dupl` clone groups dropped from 9 to 5, focused `go test ./internal/state` and `go test ./internal/tui` passed, and the test files are 284 net lines smaller.
- Deferred: Remaining `dupl` groups are cross-file render fixtures and integration fake-Codex script bodies; they are larger and riskier than this bounded pass.

## 2026-06-03 - TUI rendering dedupe

- Request: Find and implement another safe refactor that reduces lines of code and makes Weft easier to manage, scale, and reason about.
- Suggestions: Keep the current workspace/group/task UX unchanged, but make the Tasks pane renderer consume the shared `groupRowsForState` row model instead of rebuilding the new-task, ungrouped-task, group, and nested-task traversal separately from navigation; also route active current-screen and scrollback output through shared footer/visibility helpers.
- Outcome: Implemented in `.worktrees/safe-scale-refactor`; awaiting review.
- Evidence: `internal/tui/layout.go`, `internal/tui/model.go`, `internal/tui/model_shared.go`; `deadcode` was clean, and `dupl` identified duplicate task/group traversal plus active-output rendering in `internal/tui`.
- Deferred: Further duplicate cleanup candidates remain in test fixtures, but this pass intentionally kept the target bounded to TUI rendering.

## 2026-06-03 - Test-only wrapper prune

- Request: Find and implement a safe refactor that reduces lines of code and makes Weft easier to manage, scale, and reason about.
- Suggestions: Use `deadcode` to identify helpers reachable only from tests, remove thin wrappers around task creation, task-kind listing, Codex resume command construction, Codex title activity checks, terminal attention counts, frame border rendering, and direct model task creation, then keep tests pointed at the canonical functions.
- Outcome: Implemented in `.worktrees/safe-loc-refactor`; awaiting review.
- Evidence: `internal/state`, `internal/tasktypes`, `internal/codexsession`, `internal/titles`, `internal/tui`; `go run golang.org/x/tools/cmd/deadcode@latest ./...`.
- Deferred: None.

## 2026-06-03 - Refactor skill suggestion-log path correction

- Request: Update the repo-local refactor skill after the UX polish pass found the suggestion-log path was stale.
- Suggestions: Point the workflow and Suggestion Log section at `.agents/skills/weft-refactor/references/suggestion-log.md`, matching the actual checked-in file path.
- Outcome: Implemented in `.worktrees/ux-polish`; awaiting review.
- Evidence: `.agents/skills/weft-refactor/SKILL.md`, `.agents/skills/weft-refactor/references/suggestion-log.md`; `git diff --check`.
- Deferred: None.

## 2026-06-02 - Provider-neutral task metadata cutover

- Request: Continue the task type abstraction work so adding future agents such as Claude does not leave Codex-specific metadata names in the shared task schema.
- Suggestions: Hard-cut persisted task metadata from `codex_title`, `codex_status`, `codex_session_id`, and `codex_input_submitted` to provider-neutral `live_title`, `live_status`, `resume_id`, and `input_submitted`, hard-cut the title-template live-title variable from `{codex}` to `{live}`, bump the strict state schema, title-hook payload, and IPC protocol, and keep Codex-specific behavior inside the Codex task type definition.
- Outcome: Implemented in `.worktrees/task-types-abstraction`; awaiting review.
- Evidence: `internal/state`, `internal/titlehook`, `internal/ipc`, `internal/tasktypes`, `internal/tui`, `internal/codexsession`, `spec.md`, `docs/technical.md`, `docs/configuration.md`; `gofmt -w cmd internal tests`, `go test ./...`, `WEFT_RUN_INTEGRATION=1 go test ./...`, `go build ./cmd/weft`, `git diff --check`.
- Deferred: None.

## 2026-06-02 - Task type definition registry

- Request: Add a better task type abstraction so people and agents can add more task types while making the code easier to reason about, with breaking changes acceptable.
- Suggestions: Introduce a checked-in `internal/tasktypes` definition registry, make config validation ask the registry which `kind` values are supported and which integrated type ids are reserved, and move runtime task-kind policy for input mode, startup, commands, loading, status capture, terminal cwd/foreground tracking, resize, exit footers, and upgrade restartability behind the definition interface.
- Outcome: Implemented in `.worktrees/task-types-abstraction`; awaiting review.
- Evidence: `internal/tasktypes`, `internal/config/config.go`, `internal/tui`, `internal/codexsession`, `spec.md`, `docs/technical.md`; `gofmt -w cmd internal tests`, `go test ./...`, `WEFT_RUN_INTEGRATION=1 go test ./...`, `go build ./cmd/weft`.
- Deferred: None; the provider-neutral metadata cutover is tracked in the follow-up entry above.

## 2026-06-02 - Dashboard UX polish pass

- Request: Review the whole UX and implement subtle stylistic improvements to make the dashboard feel more polished, professional, and consistent.
- Suggestions: Clarify empty live-preview hints when tasks exist but no task row is selected, keep group-row counts muted as metadata, make new-task modal footer actions field-specific, use arrow glyphs in modal footer labels, and align delete-task confirmations with the Enter/Esc-only modal contract.
- Outcome: Implemented and verified in `.worktrees/ux-polish`; awaiting review.
- Evidence: `internal/tui/layout.go`, `internal/tui/task_types.go`, `internal/tui/path_prompt.go`, `internal/tui/command_menu.go`, `internal/tui/model_shared.go`, `internal/tui/layout_test.go`, `internal/tui/model_test.go`, `tests/integration/dashboard_e2e_test.go`, `spec.md`; `gofmt -w cmd internal tests`, `go test ./internal/tui`, `go test ./...`, `WEFT_RUN_INTEGRATION=1 go test ./...`, `go build ./cmd/weft`.
- Deferred: None.

## 2026-06-02 - IPC metadata hard cutover

- Request: Remove legacy unneeded code with a hard cutover, preserving the current supervisor-owned workspace/group/task model.
- Suggestions: Move client metadata out of command `Args` into typed IPC request fields, bump the IPC protocol, make old `client_id`/`width`/`height` entries inside command args fail as unsupported arguments, update `spec.md`, and make aggregate live integration waits slightly more tolerant without weakening assertions.
- Outcome: Implemented and verified in `.worktrees/legacy-prune`; awaiting review.
- Evidence: `internal/ipc/ipc.go`, `internal/supervisor/supervisor.go`, `internal/tui/client.go`, `internal/tui/client_input.go`, `internal/tui/model.go`, `tests/integration`, `spec.md`; `go run golang.org/x/tools/cmd/deadcode@latest ./...`, `gofmt -w cmd internal tests`, `go test ./...`, `WEFT_RUN_INTEGRATION=1 go test ./...`, `go build ./cmd/weft`.
- Deferred: None.

## 2026-06-01 - Strict IPC protocol cutover

- Request: Remove the legacy supervisor IPC compatibility path that accepted raw requests with missing `protocol_version`, and delete the unreachable engine-side `close_client` handler.
- Suggestions: Require every supervisor IPC request to include the current protocol version, keep `ipc.Call` auto-populating the field for current CLI/TUI clients, leave client lifecycle commands supervisor-owned, and add raw-socket coverage for missing or unsupported protocol versions.
- Outcome: Implemented in `.worktrees/strict-ipc-cutover`; awaiting review.
- Evidence: `internal/supervisor/supervisor.go`, `internal/supervisor/supervisor_test.go`, `internal/tui/model.go`, `spec.md`.
- Deferred: None.

## 2026-06-01 - Direct model UI hard cutover

- Request: Remove legacy unneeded code with a breaking hard cutover instead of keeping parity or back-compat code for this iteration.
- Suggestions: Delete the unused direct Bubble Tea `Model` UI path, old in-process modal/key handling, nav animation scaffolding, production wrappers held only by tests, stale Codex input argument adapters, and the terminal scrollback string helper; keep the supervisor engine, IPC, client dashboard, dashboard `U`, and Codex session capture/resume paths.
- Outcome: Implemented and verified in `.worktrees/legacy-cutover-cleanup`; awaiting review.
- Evidence: `internal/tui/model.go`, `internal/tui/client.go`, `internal/tui/terminal_keys.go`, `internal/tui/layout.go`, `internal/tui/task_types.go`, `internal/tui/terminal_screen.go`, and focused TUI tests; `gofmt -w cmd internal tests`, `go test ./...`, `WEFT_RUN_INTEGRATION=1 go test ./...`, `go build ./cmd/weft`, `go run golang.org/x/tools/cmd/deadcode@latest ./...`, `git diff --check`.
- Deferred: None.

## 2026-06-01 - Legacy code prune

- Request: Prune current-main legacy scaffolding and true dead helpers without merging stale cleanup worktrees or weakening dashboard `U`, supervisor-owned `upgrade_resume`, or Codex resume.
- Suggestions: Make state creation helpers require caller-provided IDs and timestamps, remove the fallback `StableID` generator and dead `groupWorkspace` helper, keep strict IPC validation while testing it with generic unexpected arguments, and refresh stale refactor-skill wording around current supervisor-owned PTY behavior.
- Outcome: Implemented and verified in `.worktrees/legacy-code-prune`; awaiting review.
- Evidence: `internal/state`, `internal/tui/model_test.go`, `spec.md`, `.agents/skills/weft-refactor/SKILL.md`; `gofmt -w cmd internal tests`, `go test ./...`, `WEFT_RUN_INTEGRATION=1 go test ./...`, `go build ./cmd/weft`, `git diff --check`, isolated dashboard smoke.
- Deferred: None.

## 2026-06-01 - Current-state hard cutover

- Request: Remove repair/backfill paths that only make sense for malformed legacy state, while preserving dashboard `U`, supervisor-owned `upgrade_resume`, Codex session capture/resume, backups, typed task launching, and current workspace/group/task UX.
- Suggestions: Delete generic `state.Repair`, make workspace/group deletion leave valid strict v5 state directly, drop the synthetic missing-task-type `[?]` UI fallback, and keep strict state/config rejection as the startup boundary.
- Outcome: Implemented in `.worktrees/current-state-hard-cutover`; awaiting review.
- Evidence: `internal/state`, `internal/tui`, `internal/app`, `internal/supervisor`, `.agents/skills/weft-refactor/references/suggestion-log.md`.
- Deferred: None.

## 2026-06-01 - Markdown hard-wrap cleanup

- Request: Remove forced line wrapping from README and other markdown, and persist the instruction in `AGENTS.md`.
- Suggestions: Unwrap normal prose and list items in tracked markdown while preserving code fences, tables, headings, and HTML blocks; add an agent rule to avoid column-width hard wrapping in markdown.
- Outcome: Implemented in `.worktrees/markdown-wrap`; awaiting review.
- Evidence: `README.md`, `CONTRIBUTING.md`, `docs/`, `spec.md`, `AGENTS.md`.

## 2026-06-01 - Maximum cutover refactor

- Request: Implement the maximum legacy cutover in a fresh `.worktrees/max-cutover-refactor` worktree, preserving dashboard `U`, supervisor-owned `upgrade_resume`, Codex session capture/resume, backups, typed task launching, and current workspace/group/task UX.
- Suggestions: Stop store load/write from repairing persisted state, validate strict v5 references/titles/statuses, reject persisted task `type_id` values missing from active config before supervisor startup, reject stale IPC `new.type_id` and `move.group_id`/`move.ungrouped`, remove state/config/release convenience wrappers held by old tests, and read Codex command/title defaults from `task_types.codex`.
- Outcome: Implemented in `.worktrees/max-cutover-refactor`; awaiting review.
- Evidence: `internal/state`, `internal/config`, `internal/app`, `internal/supervisor`, `internal/tui`, `internal/codexsession`, `internal/release`, `README.md`, `spec.md`.
- Deferred: Missing task-type display fallback removed by the current-state hard cutover.

## 2026-06-01 - Task type strictness cleanup

- Request: Remove residual task-type alias/default compatibility after the v10.0.0 strict state/config cutover.
- Suggestions: Drop hidden IPC `new` support for `type_id`, make `state.AddTaskWithType` require an explicit non-empty task type ID while keeping `state.AddTask` as the Codex-default helper, and remove the leftover `d` shortcut regression test now that current Backspace/delete-key coverage exists.
- Outcome: Implemented in `.worktrees/task-type-strictness`; awaiting review.
- Evidence: `internal/state/state.go`, `internal/tui/model.go`, `internal/state/state_test.go`, `internal/tui/model_test.go`.

## 2026-06-01 - Maximum legacy prune

- Request: Make persisted state and config strictly current-shape only, without legacy alias advice, state repair, migration, archival, or regression scaffolding for retired surfaces.
- Suggestions: Reject v5 task rows missing `type_id`, keep only generic unknown config key errors plus the current `delete = "d"` guard, remove tests that enumerate old commands/aliases/fields, and rewrite docs around the accepted current config/state contract.
- Outcome: Implemented in `.worktrees/max-legacy-prune`; awaiting review.
- Evidence: `internal/state`, `internal/config`, `internal/app`, `internal/titlehook`, `internal/titles`, `tests/integration`, `README.md`, `spec.md`.

## 2026-06-01 - Task schema cutover

- Request: Hard-cut persisted/internal `agent` schema names to `task` names without compatibility aliases, while preserving dashboard `U`, supervisor-owned `upgrade_resume`, and Codex session resume.
- Suggestions: Bump persisted state to strict v5 with `tasks`, `active_task_id`, `selected_task_id`, and focus `tasks`/`console`; reject v4/`agents` state with `weft clear` guidance; emit title-hook payload version 2 with `task_id`; rename Go helpers and IPC fields from agent to task.
- Outcome: Implemented in `.worktrees/task-schema-cutover`; awaiting review.
- Evidence: `internal/state`, `internal/ipc`, `internal/titlehook`, `internal/tui`, `tests/integration`, `README.md`, `spec.md`, `AGENTS.md`.

## 2026-06-01 - Config alias cutover

- Request: Hard-cut remaining config aliases so stale local config fails instead of silently mapping into current task configuration.
- Suggestions: Reject top-level `codex_command` and `title_template`, reject `key_bindings.new_agent`/`move_agent`, reject `task_types.*.icon`, remove the `delete = "d"` compatibility mapping, and keep generated config on current `task_types.*` plus `new_task`/`move_task`.
- Outcome: Implemented in `.worktrees/config-alias-cutover`; awaiting review.
- Evidence: `internal/config/config.go`, `internal/config/config_test.go`, `tests/integration`, `README.md`, `spec.md`.

## 2026-05-31 - Aggressive compatibility prune

- Request: Remove compatibility-only paths while preserving the current workspace/group/agent supervisor contract, backups, dashboard `U`, supervisor-owned `upgrade_resume`, and Codex session resume.
- Suggestions: Drop hidden `WEFT_HEADLESS=1` TUI behavior, stop cleaning obsolete iTerm2 Option+Backspace mappings, trust supervisor-sent upgrade state instead of synthesizing client-local upgrade state, and remove the old-supervisor `upgrade_resume` fallback copy/state.
- Outcome: Implemented in `.worktrees/aggressive-compat-prune`; awaiting review.
- Evidence: `internal/tui/client.go`, `internal/tui/terminal_keys.go`, `internal/app/doctor_keys.go`, `internal/app/app_test.go`, `internal/tui/model_test.go`, `README.md`, `spec.md`.

## 2026-05-31 - MVP legacy prune

- Request: Implement a narrow legacy/dead-code prune while preserving supervisor upgrades, dashboard `U`, `upgrade_resume`, Codex session IDs, and Codex resume.
- Suggestions: Delete stale iTerm2 map-based plist editing helpers, remove unused wrapper/helper APIs that were only test-held, keep strict unsupported legacy-shape tests, and document the current-vs-legacy boundary in `spec.md` and `AGENTS.md`.
- Outcome: Implemented in `.worktrees/mvp-legacy-prune`; awaiting review.
- Evidence: `internal/app/doctor_keys.go`, `internal/config/config.go`, `internal/runtimebackup/backup.go`, `internal/state/state.go`, `internal/tui/client.go`, `internal/supervisor/supervisor.go`, `spec.md`, `AGENTS.md`.

## 2026-05-31 - Upgrade cutover prune

- Request: Delete stale upgrade compatibility code while preserving the guarded dashboard `U` upgrade/resume flow.
- Suggestions: Remove the client-local `upgrade_resume` fallback, drop the `restart_when_idle` queued restart command family and IPC field, keep supervisor-owned `upgrade_resume` plus Codex session resume, and document the hard-cut guidance for supervisors too old to support dashboard upgrade.
- Outcome: Implemented in `.worktrees/upgrade-cutover-prune`; awaiting review.
- Evidence: `internal/tui/client.go`, `internal/supervisor/supervisor.go`, `internal/ipc/ipc.go`, `internal/tui/model_test.go`, `spec.md`.

## 2026-05-31 - Codex console parity

- Request: Assess and fix why Codex Vim mode behaves differently inside Weft, while keeping `C-b` as the dashboard escape.
- Suggestions: Replace Codex-focus key reconstruction with raw byte forwarding except the configured drawer sequence, preserve terminal-generated C-c as Codex interrupt input while Codex reports active work, keep title-hook capture as best-effort metadata, and extend the framed terminal renderer to preserve DECSCUSR cursor shape modes.
- Outcome: Implemented in `.worktrees/codex-parity-console`; awaiting review.
- Evidence: `internal/tui/client_input.go`, `internal/tui/client.go`, `internal/tui/model.go`, `internal/tui/terminal_screen.go`, `tests/integration/dashboard_e2e_test.go`, `README.md`, `spec.md`.

## 2026-05-30 - Aggressive legacy prune

- Request: Reapply the breaking legacy cleanup in a fresh `.worktrees/aggressive-legacy-prune` worktree without reusing the dirty cleanup worktree.
- Suggestions: Remove redundant `start`, hidden `tui`, and `sessions` commands; make state/config parsing strict instead of migrating or ignoring stale shapes; rename PTY internals from tab to agent; replace `internal/sessions` with `internal/pathx`.
- Outcome: Implemented in `.worktrees/aggressive-legacy-prune`; awaiting review.
- Evidence: `internal/app`, `internal/state`, `internal/config`, `internal/ptyx`, `internal/tui`, `internal/pathx`, `README.md`, `spec.md`, `tests`.

## 2026-05-30 - Aggressive legacy cleanup

- Request: Remove old tmux/workdir/folder compatibility and normalize Weft around workspaces, groups, agents, and the supervisor.
- Suggestions: Make the cleanup intentionally breaking, move state JSON to v4 `workspaces`/`groups`/`workspace_id` names, archive legacy state instead of migrating it, drop legacy config/env/CLI/title-hook aliases, and update tests/docs to match the new contract.
- Outcome: Implemented in `.worktrees/aggressive-legacy-cleanup`; awaiting review.
- Evidence: `internal/state`, `internal/config`, `internal/app`, `internal/tui`, `internal/titlehook`, `internal/titles`, `tests/integration`, `README.md`, `spec.md`.

## 2026-05-30 - Safe LOC refactor

- Request: Safely reduce Weft LOC without changing CLI, TUI, supervisor, IPC, config, state, or spec behavior.
- Suggestions: Remove the unused direct/headless TUI socket path, keep only the supervisor-driven engine command helpers, drop the legacy TUI socket from `config.Runtime` while still clearing `weft-tui.sock`, share duplicated model/client TUI selection and prompt rendering helpers, and delete unused private layout helpers.
- Outcome: Implemented and verified in `.worktrees/safe-loc-refactor`; awaiting review.
- Evidence: `internal/tui/model.go`, `internal/tui/client.go`, `internal/tui/model_shared.go`, `internal/tui/engine.go`, `internal/config/config.go`, `internal/app/app.go`, `.agents/skills/weft-refactor/references/suggestion-log.md`.

## 2026-05-30 - Dashboard form UX consistency

- Request: Refactor dashboard prompts/forms so add workdir, rename, group, move, and delete confirmation flows share one consistent professional form UX.
- Suggestions: Promote the improved add-workdir prompt into shared prompt helpers, keep path autocomplete path-specific, add useful group-name autocomplete for move-agent, prevalidate form submissions so invalid input stays open, and use compact state-specific footer actions across text and confirmation prompts.
- Outcome: Implemented in `.worktrees/forms-ux-consistency`; awaiting review.
- Evidence: `internal/tui/path_prompt.go`, `internal/tui/model.go`, `internal/tui/client.go`, `internal/tui/model_test.go`, `tests/integration/dashboard_e2e_test.go`, `README.md`, `spec.md`.

## 2026-05-30 - Framed Codex keyboard passthrough

- Request: Diagnose why proxied Codex focus input differs from a normal Codex terminal, with Shift+Enter recognized by Weft but not inserted as a Codex newline.
- Suggestions: Keep Weft's framed Codex pane active, enable enhanced terminal keyboard reporting in the attached client, forward modified-key escape sequences to the supervisor-owned Codex PTY, and reserve only the drawer key to return to the dashboard.
- Outcome: Implemented in `.worktrees/codex-shift-enter`; awaiting review.
- Evidence: `internal/tui/terminal_keys.go`, `internal/tui/client.go`, `internal/tui/model.go`, `tests/integration/dashboard_e2e_test.go`.

## 2026-05-30 - Weft rebrand

- Request: Rebrand the repo to Weft, update Homebrew publishing, GitHub remote metadata, code references, docs, and logo assets.
- Suggestions: Treat the rename as a repo-wide product boundary change: update the Go module path, CLI command directories, runtime env vars and state paths, tmux metadata, release formula renderer, docs/spec/agent instructions, and the repo-local refactor skill in one verified change.
- Outcome: Implemented in `.worktrees/rebrand-weft`; GitHub repo and local origin remote were renamed to `edwmurph/weft`.
- Evidence: `go.mod`, `cmd/weft`, `cmd/weft-release`, `internal/config`, `internal/tmuxhost`, `internal/release`, `.github/workflows/publish-homebrew.yml`, `README.md`, `spec.md`, `AGENTS.md`, `assets/weft-logo.svg`.

## 2026-05-29 - Global dashboard layout

- Request: Implement `spec.md`, preserving the working Go/tmux/PTX building blocks while redoing and extending the layout.
- Suggestions: Keep the single tmux-hosted Bubble Tea pane, terminal screen renderer, IPC loop, startup loading, and TUI-owned Codex PTYs; replace the tab/column state and renderer with global workdirs, optional flat groups inside an Agents pane, top-level agents, dashboard navigation, and full Codex focus.
- Outcome: Implemented in the original spec layout worktree; awaiting review.
- Evidence: `internal/state`, `internal/tui`, `internal/titles`, `internal/config`, `internal/tmuxhost`, `tests/integration`, `README.md`.

## 2026-05-29 - Close-Weft shortcut rename

- Request: Make `C-c` interrupt Codex first and close Weft on the next press, remove `C-q` from the shortcut surface, and merge the close-Weft CLI into `weft close`.
- Suggestions: Rename the dashboard exit binding to `close_weft`, default it to `C-c`, forward the first `C-c` to a live Codex tab in CODEX focus, arm the next `C-c` to close Weft unless another key is pressed first, make `weft close` close Weft clients when no tab id is passed, and keep legacy command compatibility outside visible docs.
- Outcome: Implemented on `main` as an unpushed pending ship commit; awaiting review after the latest behavior change.
- Evidence: `internal/config/config.go`, `internal/tui/model.go`, `tests/integration/dashboard_e2e_test.go`, `README.md`.

## 2026-05-29 - Codex-focus shortcut simplification

- Request: Stop dashboard `s` from overriding Codex input, keep only the focus toggle and close-Weft shortcut active in CODEX focus, and remove the dashboard sessions pane.
- Suggestions: Treat the focus toggle and close-Weft shortcut as the only global CODEX-focus shortcuts, forward other active-tab input to the Codex PTY, remove the dashboard sessions modal/ticker/config key, and keep session management in the CLI.
- Outcome: Implemented in `.worktrees/codex-focus-shortcuts`; awaiting review.
- Evidence: `internal/tui/model.go`, `internal/config/config.go`, `tests/integration/dashboard_e2e_test.go`, `README.md`.

## 2026-05-28 - Refactor skill Go workflow alignment

- Request: Update the repo-local refactor skill after the single-pane Go rewrite made the old Python workflow stale.
- Suggestions: Replace `pyproject.toml`/Python guidance with `go.mod`/Go guidance and update verification to gofmt, unit tests, live tmux integration, and Go build.
- Outcome: Implemented in `.worktrees/single-pane-tui-dashboard`.
- Evidence: `.agents/skills/weft-refactor/SKILL.md`.

## 2026-05-28 - Single-pane Go TUI rewrite

- Request: Replace the Python/tmux pane-composition dashboard with a Go-first single-pane TUI.
- Suggestions: Make tmux only the durable host, move Codex sessions into TUI-owned PTYs, migrate state to version 2 without tmux pane ids, add IPC for external commands, and publish Homebrew from Go source instead of Python wheels.
- Outcome: Implemented in `.worktrees/single-pane-tui-dashboard`; follow-up restored the original framed NAV/CODEX visual model and fixed empty-dashboard nav key handling.
- Evidence: `cmd/weft`, `internal/tui`, `internal/ptyx`, `internal/ipc`, `internal/state`, `.github/workflows/publish-homebrew.yml`, `tests/integration/tmux_runtime_test.go`.

## 2026-05-28 - Homebrew dependency wheelhouse

- Request: Diagnose work-network failures downloading Homebrew Python dependency resources.
- Suggestions: Stop publishing one Homebrew resource per PyPI dependency; publish a single dependency wheelhouse on the Weft GitHub release and have the tap install all dependency wheels from that archive.
- Outcome: Implemented in `.worktrees/homebrew-wheelhouse`; awaiting ship to publish a new release.
- Evidence: `.github/workflows/publish-homebrew.yml`, `scripts/render_homebrew_formula.py`, `tests/test_release_scripts.py`.

## 2026-05-28 - Homebrew publishing workflow

- Request: Publish Weft through a custom Homebrew tap and make brew the only README getting-started install path.
- Suggestions: Add a `Publish Homebrew` workflow, infer semver bumps from shipped commit text with an explicit trailer override, render the tap formula from `uv.lock`, and update ship-it instructions so publish success is part of the landing gate.
- Outcome: Implemented in `.worktrees/homebrew-publish`.
- Evidence: `.github/workflows/publish-homebrew.yml`, `scripts/next_version.py`, `scripts/render_homebrew_formula.py`, `README.md`, `CONTRIBUTING.md`, `AGENTS.md`.

## 2026-05-27 - Native Codex pane refactor

- Request: Replace the Codex PTY proxy/host renderer with native tmux panes.
- Suggestions: Launch `codex_command` directly in the content pane, keep the Kanban nav as a top interactive pane, remove proxy/pyte code, keep the previous rounded frame boxes as lightweight tmux panes, and avoid forcing Codex theme or color hints so the CLI behaves like it does in a normal PTY.
- Outcome: Implemented in `.worktrees/native-codex-pane`.
- Evidence: `weft/tmux.py`, `README.md`, `tests/test_tmux.py`, `tests/test_render.py`.

## 2026-05-27 - Repo-local refactor skill

- Request: Create a weft refactor skill that minimizes code, simplifies implementation, updates docs/instructions, finds process inefficiencies, evaluates libraries, and learns over time.
- Suggestions: Store the skill at `.agents/skills/weft-refactor`, include a durable suggestion log, and point repo agents at the skill.
- Outcome: Implemented.
- Evidence: `.agents/skills/weft-refactor/SKILL.md`, `.agents/skills/weft-refactor/agents/openai.yaml`, `.agents/skills/weft-refactor/references/suggestion-log.md`.
