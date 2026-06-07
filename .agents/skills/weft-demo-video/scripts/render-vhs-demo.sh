#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
skill_dir="$(cd "$script_dir/.." && pwd)"
repo_root="$(cd "$skill_dir/../../.." && pwd)"
tape_path="${1:-$skill_dir/tapes/readme-demo.tape}"
out_dir="${WEFT_DEMO_OUT_DIR:-/tmp/weft-demo-video}"
bin_dir="$out_dir/bin"
workspace_path="${WEFT_DEMO_WORKSPACE:-$HOME/code/weft-demo}"
workspace_marker="$workspace_path/.weft-demo-workspace"
runtime_dir="$out_dir/runtime"
config_path="$runtime_dir/config.toml"
codex_home="$out_dir/codex-home"

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
cat >> "$codex_home/config.toml" <<EOF

[projects."$escaped_workspace"]
trust_level = "trusted"
EOF

mkdir -p "$runtime_dir"
cat > "$config_path" <<EOF
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
EOF

env -u NO_COLOR CODEX_HOME="$codex_home" WEFT_HOME="$runtime_dir" WEFT_ROOT="$runtime_dir/supervisor" WEFT_WORKSPACE="$workspace_path" PATH="$bin_dir:$PATH" weft workspace add "$workspace_path" --clear >/dev/null
env -u NO_COLOR CODEX_HOME="$codex_home" WEFT_HOME="$runtime_dir" WEFT_ROOT="$runtime_dir/supervisor" WEFT_WORKSPACE="$workspace_path" PATH="$bin_dir:$PATH" weft group add refactor >/dev/null
env -u NO_COLOR CODEX_HOME="$codex_home" WEFT_HOME="$runtime_dir" WEFT_ROOT="$runtime_dir/supervisor" WEFT_WORKSPACE="$workspace_path" PATH="$bin_dir:$PATH" weft group add checks >/dev/null

env -u NO_COLOR CODEX_HOME="$codex_home" WEFT_HOME="$runtime_dir" WEFT_ROOT="$runtime_dir/supervisor" WEFT_WORKSPACE="$workspace_path" PATH="$bin_dir:$PATH" vhs validate "$tape_path"
env -u NO_COLOR CODEX_HOME="$codex_home" WEFT_HOME="$runtime_dir" WEFT_ROOT="$runtime_dir/supervisor" WEFT_WORKSPACE="$workspace_path" PATH="$bin_dir:$PATH" vhs "$tape_path"
