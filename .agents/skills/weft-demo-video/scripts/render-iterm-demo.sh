#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
skill_dir="$(cd "$script_dir/.." && pwd)"
repo_root="$(cd "$skill_dir/../../.." && pwd)"
out_dir="${WEFT_DEMO_OUT_DIR:-/tmp/weft-demo-video}"
bin_dir="$out_dir/bin"
workspace_path="${WEFT_DEMO_WORKSPACE:-$HOME/code/weft-demo}"
workspace_marker="$workspace_path/.weft-demo-workspace"
runtime_dir="$out_dir/iterm-runtime"
config_path="$runtime_dir/config.toml"
codex_home="$out_dir/iterm-codex-home"
output_path="${WEFT_DEMO_NATIVE_OUT:-$out_dir/weft-demo-readme-iterm.mp4}"
screen_device="${WEFT_DEMO_SCREEN_DEVICE:-3}"
screen_scale="${WEFT_DEMO_SCREEN_SCALE:-2}"
capture_seconds="${WEFT_DEMO_CAPTURE_SECONDS:-130}"
window_left="${WEFT_DEMO_WINDOW_LEFT:-80}"
window_top="${WEFT_DEMO_WINDOW_TOP:-80}"
window_width="${WEFT_DEMO_WINDOW_WIDTH:-1280}"
window_height="${WEFT_DEMO_WINDOW_HEIGHT:-720}"

require() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

apple_quote() {
  local value="$1"
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  printf '"%s"' "$value"
}

shell_quote() {
  printf "%q" "$1"
}

require ffmpeg
require go
require osascript

mkdir -p "$bin_dir" "$out_dir"
if [[ -z "${WEFT_DEMO_WORKSPACE:-}" ]]; then
  if [[ -L "$workspace_path" ]]; then
    unlink "$workspace_path"
  fi
  if [[ -e "$workspace_path" && ! -e "$workspace_marker" ]]; then
    echo "demo workspace path exists and is not managed by this script: $workspace_path" >&2
    exit 1
  fi
  mkdir -p "$workspace_path"
  rsync -a --delete \
    --exclude ".env" \
    --exclude ".git" \
    --exclude ".weft" \
    --exclude ".weft-demo-workspace" \
    --exclude ".weft-runtime" \
    --exclude ".worktrees" \
    "$repo_root/" "$workspace_path/"
  touch "$workspace_marker"
fi

go -C "$repo_root" build -o "$bin_dir/weft" ./cmd/weft

escaped_workspace="${workspace_path//\\/\\\\}"
escaped_workspace="${escaped_workspace//\"/\\\"}"

mkdir -p "$codex_home"
if [[ -f "$HOME/.codex/auth.json" ]]; then
  cp "$HOME/.codex/auth.json" "$codex_home/auth.json"
fi
if [[ -f "$HOME/.codex/config.toml" ]]; then
  cp "$HOME/.codex/config.toml" "$codex_home/config.toml"
else
  : > "$codex_home/config.toml"
fi
cat >> "$codex_home/config.toml" <<EOF_CONFIG

[projects."$escaped_workspace"]
trust_level = "trusted"
EOF_CONFIG

mkdir -p "$runtime_dir"
cat > "$config_path" <<EOF_CONFIG
default_task_type = "codex"

[task_types.codex]
label = "Codex"
kind = "codex"
command = "codex"
badge = "[codex]"
title_template = "{live}"

[task_types.shell]
label = "Shell"
kind = "terminal"
command = "exec \"\$SHELL\" -l"
badge = "[shell]"
title_template = "Shell"
EOF_CONFIG

demo_env=(
  env -u NO_COLOR
  CODEX_HOME="$codex_home"
  WEFT_HOME="$runtime_dir"
  WEFT_ROOT="$runtime_dir/supervisor"
  WEFT_WORKSPACE="$workspace_path"
  PATH="$bin_dir:$PATH"
)

"${demo_env[@]}" weft workspace add "$workspace_path" --clear >/dev/null
"${demo_env[@]}" weft group add refactor >/dev/null
"${demo_env[@]}" weft group add checks >/dev/null

right=$((window_left + window_width))
bottom=$((window_top + window_height))
crop_x=$((window_left * screen_scale))
crop_y=$((window_top * screen_scale))
crop_w=$((window_width * screen_scale))
crop_h=$((window_height * screen_scale))
setup_script="$out_dir/iterm-demo-setup.scpt"
driver_script="$out_dir/iterm-demo-driver.scpt"

{
  echo 'tell application "iTerm"'
  echo '  activate'
  echo '  set demoWindow to (create window with default profile)'
  echo "  set bounds of demoWindow to {$window_left, $window_top, $right, $bottom}"
  echo '  tell current session of demoWindow'
  echo "    write text $(apple_quote "cd $(shell_quote "$workspace_path")")"
  echo "    write text $(apple_quote "export PATH=$(shell_quote "$bin_dir"):\$PATH")"
  echo "    write text $(apple_quote "export CODEX_HOME=$(shell_quote "$codex_home")")"
  echo "    write text $(apple_quote "export WEFT_HOME=$(shell_quote "$runtime_dir")")"
  echo "    write text $(apple_quote "export WEFT_ROOT=$(shell_quote "$runtime_dir/supervisor")")"
  echo "    write text $(apple_quote "export WEFT_WORKSPACE=$(shell_quote "$workspace_path")")"
  echo "    write text $(apple_quote "export COLORTERM=truecolor")"
  echo "    write text $(apple_quote "export FORCE_COLOR=1")"
  echo "    write text $(apple_quote "export CLICOLOR_FORCE=1")"
  echo "    write text $(apple_quote "unset NO_COLOR")"
  echo "    write text $(apple_quote "export PROMPT='$ '")"
  echo "    write text $(apple_quote "export RPROMPT=''")"
  echo "    write text $(apple_quote "clear")"
  echo '  end tell'
  echo 'end tell'
} > "$setup_script"

cat > "$driver_script" <<'EOF_APPLESCRIPT'
on sendBytes(theText)
  tell application "iTerm"
    tell current session of front window
      write text theText newline no
    end tell
  end tell
end sendBytes

on pressReturn()
  tell application "iTerm"
    tell current session of front window
      write text ""
    end tell
  end tell
end pressReturn

on pressEscape()
  sendBytes(ASCII character 27)
end pressEscape

on pressDown()
  sendBytes((ASCII character 27) & "[B")
end pressDown

on pressUp()
  sendBytes((ASCII character 27) & "[A")
end pressUp

on pressRight()
  sendBytes((ASCII character 27) & "[C")
end pressRight

on pressShiftTab()
  sendBytes((ASCII character 27) & "[Z")
end pressShiftTab

on pressControlB()
  sendBytes(ASCII character 2)
end pressControlB

on typeText(theText)
  sendBytes(theText)
end typeText

tell application "iTerm" to activate
delay 1

typeText("weft")
pressReturn()
delay 8

typeText("n")
delay 2
pressDown()
delay 2
pressDown()
delay 2
pressReturn()
delay 6
typeText("Find one tiny TUI rendering cleanup and run a narrow check.")
delay 2
pressReturn()
delay 2
pressControlB()
delay 4
typeText("m")
delay 2
typeText("refactor")
delay 2
pressReturn()
delay 3

typeText("n")
delay 2
pressRight()
delay 2
pressDown()
delay 2
pressDown()
delay 2
pressReturn()
delay 4
typeText("go test ./internal/titlehook")
delay 2
pressReturn()
delay 2
pressControlB()
delay 4
typeText("m")
delay 2
typeText("checks")
delay 2
pressReturn()
delay 3

typeText("n")
delay 2
pressDown()
delay 2
pressDown()
delay 2
pressReturn()
delay 6
pressShiftTab()
delay 2
typeText("Plan README demo upkeep. Do not edit files.")
delay 2
pressReturn()
delay 2
pressControlB()
delay 4
typeText("m")
delay 2
typeText("refactor")
delay 2
pressReturn()
delay 3

pressDown()
delay 3
pressDown()
delay 3
pressUp()
delay 3
typeText("?")
delay 3
pressEscape()
delay 2
pressDown()
delay 4
EOF_APPLESCRIPT

osascript "$setup_script"
sleep 1

ffmpeg_pid=""
cleanup() {
  if [[ -n "$ffmpeg_pid" ]] && kill -0 "$ffmpeg_pid" >/dev/null 2>&1; then
    kill "$ffmpeg_pid" >/dev/null 2>&1 || true
    wait "$ffmpeg_pid" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

ffmpeg -hide_banner -y \
  -f avfoundation \
  -framerate 30 \
  -capture_cursor 0 \
  -i "$screen_device:none" \
  -t "$capture_seconds" \
  -vf "crop=${crop_w}:${crop_h}:${crop_x}:${crop_y}" \
  -pix_fmt yuv420p \
  "$output_path" &
ffmpeg_pid=$!

sleep 1
osascript "$driver_script"
wait "$ffmpeg_pid"
trap - EXIT

echo "$output_path"
