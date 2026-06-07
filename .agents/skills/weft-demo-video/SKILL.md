---
name: weft-demo-video
description: Use when recording, regenerating, editing, or planning the public Weft demo video; reproducing the README demo; adding keyboard shortcut overlays; or deciding whether product/demo changes require a refreshed demo asset.
---

# Weft Demo Video

## Purpose

Create a real, terminal-only Weft demo that shows parallel Codex and shell work in the current product. The demo should be reproducible, privacy-safe, and paced for new users who have not seen Weft before.

## Required Tools

- Prefer native iTerm capture (`scripts/render-iterm-demo.sh`) when the demo must match the user's current iTerm default profile. It records the real terminal with AppleScript plus `ffmpeg`, so font, line height, theme, and terminal background come from iTerm instead of a terminal-rendering library.
- Use VHS (`vhs`) when deterministic, headless, code-reviewed capture matters more than exact local terminal-profile matching. VHS scripts the demo as a `.tape`, renders through `ttyd`, and outputs MP4 directly through `ffmpeg`.
- Use `$hyperframes` only for post-production that should remain outside the terminal recording itself: keyboard overlays, title-safe framing, light trims, or final MP4 composition.
- Use `$computer-use:computer-use` only as a fallback when the scripted recorders cannot reproduce a real terminal behavior or when the user explicitly asks for a native manual recording.
- Use the current Weft repo/worktree requested by the user. If they do not specify one, record against current `main`.

## Workflow

1. Read `README.md` and `references/agenda.md` to confirm the current public demo framing.
2. Prepare all setup outside the recording: build/install the intended Weft version, configure a disposable Weft runtime, and hide personal shell prompt details.
3. For new recordings that need exact local terminal styling, start with `scripts/render-iterm-demo.sh`. For deterministic headless runs, start from `tapes/readme-demo.tape` and run `scripts/render-vhs-demo.sh` from this skill directory or pass it a tape path. Both wrappers build the current checkout as a temporary `weft` binary and seed a disposable workspace plus the demo groups before capture so the visible startup command can stay public-facing as `weft`.
4. Keep the real `HOME` during recording so Codex uses normal shell behavior. Use a temporary `CODEX_HOME` copied from the user's Codex auth/config with the disposable demo workspace marked trusted, so the recording does not show Codex setup prompts and does not mutate global Codex config.
5. Run the recorder with `NO_COLOR` unset. Codex agent shells may set `NO_COLOR=1`, and `termenv` treats that as stronger than `CLICOLOR_FORCE`, which makes Weft's lipgloss colors render as plain white in the video.
6. Match the user's iTerm default profile in the recorder, not by adding demo-only color hooks to Weft. Native iTerm capture should use the actual default profile. VHS should use the profile's JSON palette, `FontFamily "Monaco"`, matching line height, and matching terminal padding/background.
7. Keep the tape as the source of truth for visible actions and minimum holds. Every command that changes screen, focus, selection, or dialog state should be followed by a `Sleep 2s` or a longer explicit hold after the new state is visible.
8. Record one continuous take when possible. If trimming is needed, trim idle time, not interaction frames. Avoid speed-ups, jump cuts, zoom-ins, terminal resizes, and theme/font changes.
9. Follow the agenda in `references/agenda.md`, adapting prompts only when the repo has a more useful current maintenance task.
10. Build a keyboard cue sheet from the rendered raw video, not just from tape sleeps. Overlay intentional commands and shortcuts such as `weft`, `n`, `Enter`, `m refactor`, `Ctrl-B`, `Shift-Tab`, `?`, arrow keys, and submitted shell commands.
11. Render the final video and inspect it at normal playback speed before updating README media.
12. Before staging any README or media update, separate or discard unrelated changes produced by demo Codex tasks.

## Recorder Choice

Use native iTerm capture when the user asks for the video to look like their current iTerm profile. Use VHS when repeatability and CI-friendly scripting are the priority. Do not keep adding timed pixel masks to compensate for a recorder that renders terminal state differently from the real Weft TUI.

Re-check current project activity before switching tools, but use these criteria:

- Native iTerm capture: best fit for matching a user's real iTerm profile. It is less portable and requires macOS/iTerm/AppleScript screen-capture access, but it avoids library-level differences in font weight, line height, and profile background.
- VHS: best deterministic fallback for this demo. It is scriptable, validates tapes, controls terminal theme/font/line-height/size/padding, and writes MP4/WebM/GIF.
- asciinema plus agg: good for recording terminal sessions and generating GIFs, but less direct for a fully scripted interactive TUI demo with precise input pacing and MP4 delivery.
- Terminalizer: scriptable and customizable, but less attractive when current activity lags VHS.
- termtosvg or termsvg: useful for SVG/GIF terminal captures, but not a strong fit for this README MP4 workflow.

If the recorder still renders Codex differently from the user's real terminal, first reproduce the same state manually in normal Weft using the current checkout and inspect Codex's raw PTY output when the source of the styling is unclear. If raw Codex emits a real background and Weft drops it, fix Weft's terminal parser as product code. If raw Codex uses the default background for the prompt row but the public demo requires Weft's nested Codex panes to preserve a grey prompt box, use Weft's Codex-only input-row guide. Do not add demo-only environment variables or timed pixel masks.

## Quality Bar

- Capture the whole terminal window for the whole video. Do not zoom, crop to a pane, or change terminal size mid-recording.
- Keep visual continuity. Terminals should not suddenly look different between shots.
- Hold important screens long enough to digest: 2-4 seconds on the dashboard after task creation, 2 seconds on filled forms before submit, 2-3 seconds per live preview switch, and 3 seconds on help.
- Treat every visible command that changes screen, focus, selection, or dialog state as a state transition. After the new state appears, hold it for at least 2 seconds before the next command. This includes opening task or move forms, changing the task-type selector, returning to the dashboard with `Ctrl-B`, opening/closing help, typing a move target before `Enter`, and submitting prompts before navigating away. The underlying recording must pause on the visible state; do not rely on keyboard overlay spacing or post-production timing alone.
- Keep the demo reasonably short, but do not make it skippy. Aim for 90-120 seconds; prefer clarity over forcing every take under 2 minutes.
- Show exactly two groups and three tasks: two Codex tasks and one shell task. Show at least one active/loading state, one live preview update, and one task that needs attention or is ready for follow-up.
- Demonstrate groups inside the default/main workspace. Do not create another Weft workspace unless the user explicitly asks for a workspace-focused demo.
- Keep submitted Codex prompts concise enough for the recorded Task Live Preview width unless the demo is explicitly validating wrapping behavior. Long prompts that wrap in the narrow preview can make the recording look worse than normal manual use, even when the app renders correctly at the user's everyday terminal size.
- Demonstrate plan mode with `Shift-Tab`, then return to the dashboard and show the parallel work state. Do not end by submitting a follow-up instruction while the plan task is still streaming; if the plan is not clearly ready, end on the dashboard instead of opening the task.
- Demonstrate the shell task with a real command, for example `go test ./internal/titlehook`, then returning immediately to the dashboard. Do not add artificial `sleep` commands just to keep the task alive.
- Avoid personal information: no desktop, email, browser tabs, long absolute paths, secrets, private filenames outside this repo, or custom shell history.

## Terminal Render Matching

If the terminal recording or rendering library shows Codex panes differently than the real Weft TUI, first fix the recorder palette/environment or switch to native iTerm capture. Do not add demo-only terminal color hooks or post-production prompt-box masks. Before changing app code, verify whether the difference comes from raw Codex ANSI or Weft's nested terminal rendering.

When matching the user's iTerm default profile in VHS, do not rely only on `Set Theme "iTerm2 Default"` because the renderer may still use black for the terminal canvas/padding. Use an explicit JSON theme palette from the profile and set `MarginFill`/terminal background to the same profile background. Verify sampled pixels: outer padding/dashboard should match the iTerm background, while Codex input boxes should remain visibly lighter grey-blue than the base pane if they appear in the captured terminal output.

Codex input boxes must be terminal-aware, not time-positioned drawboxes. Current Codex may render its idle prompt with default background (`49m`) rather than a distinct background color; in Weft's nested task console/preview, the grey prompt box is therefore supplied by the Codex-only input-row guide. Keep that guide small: it should detect `›` prompt rows, paint the prompt row plus one row of padding, fill the blank suffix, derive the grey from the pane's default background, apply to current screen and scrollback, and never apply to shell task output. Keep focused tests for Codex-on/shell-off behavior.

Validate render corrections with sampled pixels and extracted frames for the full Codex console, dashboard preview, move/new-task dialogs, cleared/no-selection previews, and help. Do not simulate Codex input backgrounds with timed fixed-position drawboxes; they drift relative to terminal content during transitions. Prefer a capture/render path that preserves the terminal's own ANSI background attributes, or fall back to a known-good checkpoint artifact instead of masking the video by time.

For Codex task-preview checks, verify that typed input boxes remain empty until text is actually typed, prior prompt backgrounds cover wrapped prompt text plus the bottom padding row, and no stale user-entered prompt text appears while the previous message is still Working. If a prior user prompt wraps, its grey prompt box needs an extra terminal row of bottom padding in task preview so the wrapped text does not feel cramped. Treat that padding as part of the prompt box itself: the whole grey box should appear and disappear as one unit, with one visibility window, rather than adding or removing a separately timed bottom row.

Do not redact normal Codex placeholder text just because it is visible. Placeholder text in every Codex input surface, including task console, task preview, Working tasks, and Ready tasks, should initialize muted/dim and clearly lower contrast than typed user input; it must not render as bright white even for a single frame before settling. Validate all later Codex preview states too, not only the first task: after every Codex task creation, submitted prompt, dashboard return, group move such as `m refactor`, status change, output growth, selected-task change, and help/dialog transition, inspect the first rendered frame and the settled frame. If post-production correction is necessary, cover every transient prompt location so placeholder styling never flashes in the wrong color before settling.

When a Codex prompt box moves because task-preview content scrolls or status changes, correction regions must track the actual source frame positions. Extract source and corrected frames immediately before, at, and after every prompt-location handoff and exit frame. Do not let a crop for the old prompt position stay active after text or the input box has moved, because it can dim unrelated content or leave a bottom-row rectangle. FFmpeg `between()` includes both endpoints, so avoid overlapping adjacent correction windows; prefer frame-step windows such as ending the old crop at the last verified frame and starting the next crop at the first verified frame.

Task-preview panes are passive snapshots and must never show a live Codex insertion cursor, including when the bottom prompt box is empty or when placeholder text is visible. The active Task Console may show the cursor while the user is actually typing, but once a prompt is submitted and Codex is Working, the empty or placeholder prompt box should not retain a white bar cursor in the demo. Validate cursor absence on the first Working frame after each Codex submit, the settled Working frame, every dashboard preview frame after returning with `Ctrl-B`, and the first frame after moving a task between groups.

## Keyboard Overlays

Use concise keycap-style overlays rather than captions. Place them consistently near the lower right or lower center, outside the active input line when possible.

Overlay only meaningful actions:

- Startup command: `weft` or `weft --clear`, matching the visible command in the raw recording
- Dashboard commands: `n`, `?`, arrow keys
- Form and console submits: `Enter`
- Return to dashboard: `Ctrl-B`
- Plan mode toggle: `Shift-Tab`
- Shell command submit: show the submitted command as one label, not every typed character

Each overlay should last about 0.8-1.5 seconds. For multi-key shortcuts, show the combined shortcut once.

Before final delivery, inspect the video at normal speed and confirm each overlay lines up with the actual UI action and that no screen/dialog transition advances within 2 seconds of the previous transition.

For every keyboard overlay, keep a cue row with label, start, end, and the visible-state anchor it must match. After rendering overlays, generate a validation contact sheet with one labeled midpoint frame per overlay. Extract validation frames with accurate seeking against the final rendered video; when using `ffmpeg`, place `-ss` after `-i` for these checks instead of using fast pre-input seeking. A cue passes only when its midpoint frame shows the intended action/result: for dialog labels, the dialog must be open; for labels that include typed targets such as `m refactor`, the filled target must be visible; for submits, the submitted text or resulting screen must be visible. If any cue fails, retime and rerender before delivery.

## Keeping It Current

If the demo agenda, task prompts, shown groups, task types, dashboard statuses, README Demo section, or public media asset changes, explicitly recommend regenerating the demo video in the final response. The README should not point at a stale workflow after product behavior changes.

When delivering a refreshed video, report:

- Source Weft commit or release used for recording
- Final video path or hosted URL
- README Demo section change, if any
- Whether keyboard overlays were added
- Any known continuity or pacing compromises
