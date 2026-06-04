---
name: weft-demo-video
description: Use when recording, regenerating, editing, or planning the public Weft demo video; reproducing the README demo; adding keyboard shortcut overlays; or deciding whether product/demo changes require a refreshed demo asset.
---

# Weft Demo Video

## Purpose

Create a real, terminal-only Weft demo that shows parallel Codex and shell work in the current product. The demo should be reproducible, privacy-safe, and paced for new users who have not seen Weft before.

## Required Tools

- Use `$computer-use:computer-use` to operate the real terminal and Weft UI.
- Use `$hyperframes` for post-production when adding keyboard overlays, title-safe framing, trims, or final MP4 rendering.
- Use the current Weft repo/worktree requested by the user. If they do not specify one, record against current `main`.

## Workflow

1. Read `README.md` and `references/agenda.md` to confirm the current public demo framing.
2. Prepare all setup outside the recording: build/install the intended Weft version, configure a disposable Weft runtime, and hide personal shell prompt details.
3. Start the capture from a fresh terminal. The only visible shell command should be the command that starts Weft.
4. Record one continuous take when possible. If trimming is needed, trim idle time, not interaction frames. Avoid speed-ups, jump cuts, zoom-ins, terminal resizes, and theme/font changes.
5. Follow the agenda in `references/agenda.md`, adapting prompts only when the repo has a more useful current maintenance task.
6. Build a keyboard cue sheet while recording. Overlay intentional commands and shortcuts such as `weft`, `n`, `Enter`, `Ctrl-B`, `Shift-Tab`, `?`, arrow keys, and submitted shell commands.
7. Render the final video and inspect it at normal playback speed before updating README media.
8. Before staging any README or media update, separate or discard unrelated changes produced by demo Codex tasks.

## Quality Bar

- Capture the whole terminal window for the whole video. Do not zoom, crop to a pane, or change terminal size mid-recording.
- Keep visual continuity. Terminals should not suddenly look different between shots.
- Hold important screens long enough to digest: 2-4 seconds on the dashboard after task creation, 2 seconds on filled forms before submit, 2-3 seconds per live preview switch, and 3 seconds on help.
- Keep the demo reasonably short, but do not make it skippy. Aim for 90-120 seconds; prefer clarity over forcing every take under 2 minutes.
- Show exactly two groups and three tasks: two Codex tasks and one shell task. Show at least one active/loading state, one live preview update, and one task that needs attention or is ready for follow-up.
- Demonstrate groups inside the default/main workspace. Do not create another Weft workspace unless the user explicitly asks for a workspace-focused demo.
- Demonstrate plan mode with `Shift-Tab`, then show the dashboard state that indicates the plan is ready. End by opening that task and entering the follow-up instruction to implement the plan.
- Demonstrate the shell task while it is still running, for example by running tests followed by a short `sleep`, then returning immediately to the dashboard.
- Avoid personal information: no desktop, email, browser tabs, long absolute paths, secrets, private filenames outside this repo, or custom shell history.

## Keyboard Overlays

Use concise keycap-style overlays rather than captions. Place them consistently near the lower right or lower center, outside the active input line when possible.

Overlay only meaningful actions:

- Startup command: `weft` or `weft --clear`
- Dashboard commands: `n`, `?`, arrow keys
- Form and console submits: `Enter`
- Return to dashboard: `Ctrl-B`
- Plan mode toggle: `Shift-Tab`
- Shell command submit: show the submitted command as one label, not every typed character

Each overlay should last about 0.8-1.5 seconds. For multi-key shortcuts, show the combined shortcut once.

## Keeping It Current

If the demo agenda, task prompts, shown groups, task types, dashboard statuses, README Demo section, or public media asset changes, explicitly recommend regenerating the demo video in the final response. The README should not point at a stale workflow after product behavior changes.

When delivering a refreshed video, report:

- Source Weft commit or release used for recording
- Final video path or hosted URL
- README Demo section change, if any
- Whether keyboard overlays were added
- Any known continuity or pacing compromises
