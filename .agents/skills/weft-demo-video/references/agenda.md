# Weft Demo Recording Agenda

Use this as the canonical public demo flow. Adapt task prompts when a current repo maintenance need would make a more useful real demo, but keep the same product beats unless the README framing changes.

## Preflight Outside The Recording

- Use current `main` unless the user explicitly requests another worktree or release.
- Use a disposable runtime so no personal Weft state appears:
  - `WEFT_HOME=/tmp/weft-demo-runtime`
  - `WEFT_ROOT=/tmp/weft-demo-runtime/supervisor`
- Build or install Weft before capture. Do not show setup, build, cleanup, or Git commands in the recording.
- When using a recorder wrapper, seed the default/main workspace plus groups `refactor` and `checks` before capture. The demo shows task movement into existing groups, not group creation.
- Keep the real `HOME` and use a temporary `CODEX_HOME` copied from normal Codex auth/config with the demo workspace marked trusted. The recording should not show Codex login or trust setup prompts.
- Unset `NO_COLOR` for the recorder process so Weft's own dashboard colors are captured.
- Match the user's iTerm default profile in the recorder. Native iTerm capture should use the real default profile; VHS should use the profile palette in the tape. Do not pass demo-only terminal color environment variables to Weft just to make a recorder palette line up.
- Preserve Codex's lighter grey prompt/input box in task console and task preview. Verify it appears on current and prior Codex prompt rows, is lighter than the pane background, and is not applied to shell task output.
- Use a neutral terminal prompt and a fixed terminal size. Clear the screen before recording starts.
- Start recording only after setup is complete.
- Use the default/main workspace. This demo should show groups inside one workspace, not workspace creation.

## Visible Demo Flow

Target length: 90-120 seconds.

1. Start from a fresh terminal.
   - Visible command: `weft` when the disposable workspace is seeded before capture, or `weft --clear` when the recording intentionally shows first-run setup.
   - Hold the initial dashboard for about 3 seconds.
   - Overlay: `weft` or `weft --clear`, matching the visible command.

2. Create the first Codex task in group `refactor`.
   - Press `n`, choose `Codex`.
   - Prompt: `Find one tiny TUI rendering cleanup and run a narrow check.`
   - Press `Enter` to submit, then immediately press `Ctrl-B`.
   - Hold dashboard at least 2 seconds so the viewer sees auto-title generation and the live preview update.
   - Press `m`, type `refactor`, hold the filled move form for at least 2 seconds, then press `Enter`.
   - Hold the dashboard at least 2 seconds after the task moves into the group.
   - Overlays: `n`, `Enter`, `Ctrl-B`, `m refactor`, `Enter`.

3. Create a shell task in group `checks`.
   - Press `n`, choose `Shell`.
   - Command: `go test ./internal/titlehook`
   - Press `Enter` to submit, then immediately press `Ctrl-B`.
   - Hold dashboard while the shell task is active/loading.
   - Press `m`, type `checks`, hold the filled move form for at least 2 seconds, then press `Enter`.
   - Hold the dashboard at least 2 seconds after the task moves into the group.
   - Overlays: `n`, `Enter`, `Ctrl-B`, `go test ./internal/titlehook`, `m checks`, `Enter`.

4. Create a plan-mode Codex task in group `refactor`.
   - Press `n`, choose `Codex`.
   - Press `Shift-Tab` to enter plan mode before sending the prompt.
   - Prompt: `Plan README demo upkeep. Do not edit files.`
   - Press `Enter` to submit, then immediately press `Ctrl-B`.
   - Hold dashboard at least 2 seconds so the plan task title and preview start changing.
   - Press `m`, type `refactor`, hold the filled move form for at least 2 seconds, then press `Enter`.
   - Hold the dashboard at least 2 seconds after the task moves into the group.
   - Overlays: `n`, `Shift-Tab`, `Enter`, `Ctrl-B`, `m refactor`, `Enter`.

5. Show parallel work from the dashboard.
   - Move selection across the three tasks.
   - Hold each selected task for 2-3 seconds so the live preview is readable.
   - Show both groups visible at once.
   - Overlay only navigation keys that matter, such as `Up` or `Down`.

6. Show help without breaking flow.
   - Press `?`.
   - Hold help for about 3 seconds.
   - Close help with `Esc` or return to the dashboard.
   - Overlays: `?`, `Esc`.

7. End on the dashboard.
   - Wait until one task completes or needs attention.
   - Keep the dashboard visible for at least 3 seconds after the status changes or after closing help so the final state is readable.
   - Do not open the plan task or submit `Implement the plan.` unless the plan is visibly ready before that segment starts. If the plan is still streaming, stop on the dashboard instead.

## Continuity Rules

- Use the same terminal window, font, color theme, and size for the entire capture.
- Do not cut between a prompt submit and the dashboard state it produces.
- Do not speed up typing or task output. If the demo runs long, remove dead waiting time between meaningful states.
- If a task stalls, keep the dashboard visible only long enough to show status, then switch previews or help. Do not leave a static loading dashboard for more than 8-10 seconds.
- If Codex auto-title fails or a task does not submit, redo the take instead of hiding the problem with edits.

## Cue Sheet Format

Create a simple cue sheet during or immediately after the raw capture. Use relative timestamps from the final raw clip.

```json
[
  { "time": 3.2, "duration": 1.2, "label": "weft" },
  { "time": 9.5, "duration": 0.9, "label": "n" },
  { "time": 22.1, "duration": 1.0, "label": "Enter" },
  { "time": 22.9, "duration": 1.0, "label": "Ctrl-B" },
  { "time": 45.0, "duration": 1.4, "label": "go test ./internal/titlehook" },
  { "time": 63.4, "duration": 1.0, "label": "Shift-Tab" }
]
```

Use HyperFrames to render the overlays from the cue sheet onto the raw screen recording. Do a normal-speed review pass after rendering and adjust cue timing if an overlay lands before the visible action.
