# Weft Demo Recording Agenda

Use this as the canonical public demo flow. Adapt task prompts when a current repo maintenance need would make a more useful real demo, but keep the same product beats unless the README framing changes.

## Preflight Outside The Recording

- Use current `main` unless the user explicitly requests another worktree or release.
- Use a disposable runtime so no personal Weft state appears:
  - `WEFT_HOME=/tmp/weft-demo-runtime`
  - `WEFT_ROOT=/tmp/weft-demo-runtime/supervisor`
- Build or install Weft before capture. Do not show setup, build, cleanup, or Git commands in the recording.
- Use a neutral terminal prompt and a fixed terminal size. Clear the screen before recording starts.
- Start recording only after setup is complete.
- Use the default/main workspace. This demo should show groups inside one workspace, not workspace creation.

## Visible Demo Flow

Target length: 90-120 seconds.

1. Start from a fresh terminal.
   - Visible command: `weft --clear` or the shortest command that starts the intended Weft build.
   - Hold the empty dashboard for about 3 seconds.
   - Overlay: `weft --clear`.

2. Create the first Codex task in group `refactor`.
   - Press `n`, choose `Codex`, set group `refactor`.
   - Prompt: `Find one small readability cleanup in the TUI rendering code, keep the diff tiny, and run the narrowest useful check.`
   - Press `Enter` to submit, then immediately press `Ctrl-B`.
   - Hold dashboard at least 2 seconds so the viewer sees auto-title generation and the live preview update.
   - Overlays: `n`, `Enter`, `Ctrl-B`.

3. Create a shell task in group `checks`.
   - Press `n`, choose `Shell`, set group `checks`.
   - Command: `go test ./... && sleep 10`
   - Press `Enter` to submit, then immediately press `Ctrl-B`.
   - Hold dashboard while the shell task is active/loading.
   - Overlays: `n`, `Enter`, `Ctrl-B`, `go test ./... && sleep 10`.

4. Create a plan-mode Codex task in group `refactor`.
   - Press `n`, choose `Codex`, set group `refactor`.
   - Press `Shift-Tab` to enter plan mode before sending the prompt.
   - Prompt: `Make a short plan for keeping the README demo video current as Weft workflows evolve. Do not edit files yet.`
   - Press `Enter` to submit, then immediately press `Ctrl-B`.
   - Hold dashboard at least 2 seconds so the plan task title and preview start changing.
   - Overlays: `n`, `Shift-Tab`, `Enter`, `Ctrl-B`.

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

7. Show completion or attention.
   - Wait until one task completes or needs attention.
   - Keep the dashboard visible for at least 3 seconds after the status changes so the attention cue is obvious.
   - Select the plan task once it is ready.

8. End on an actionable follow-up.
   - Open the plan task.
   - Enter: `Implement the plan.`
   - Press `Enter`.
   - Return to the dashboard or end on the submitted console.
   - Overlays: `Enter`, `Ctrl-B` if returning.

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
  { "time": 3.2, "duration": 1.2, "label": "weft --clear" },
  { "time": 9.5, "duration": 0.9, "label": "n" },
  { "time": 22.1, "duration": 1.0, "label": "Enter" },
  { "time": 22.9, "duration": 1.0, "label": "Ctrl-B" },
  { "time": 45.0, "duration": 1.4, "label": "go test ./... && sleep 10" },
  { "time": 63.4, "duration": 1.0, "label": "Shift-Tab" }
]
```

Use HyperFrames to render the overlays from the cue sheet onto the raw screen recording. Do a normal-speed review pass after rendering and adjust cue timing if an overlay lands before the visible action.
