#!/bin/sh
set -eu

usage() {
	printf 'Usage: %s <slug> [ref]\n' "$0" >&2
	printf 'Creates or reuses .worktrees/<slug>, then links local .env and config.toml.\n' >&2
}

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
	usage
	exit 2
fi

slug=$1
ref=${2:-main}

case "$slug" in
	"" | /* | *"/"* | *".."*)
		printf 'Invalid worktree slug: %s\n' "$slug" >&2
		exit 2
		;;
esac

git_common_dir=$(git rev-parse --path-format=absolute --git-common-dir)
repo_root=$(cd "$git_common_dir/.." && pwd -P)
worktree=$repo_root/.worktrees/$slug

if [ -d "$worktree" ]; then
	printf 'Using existing worktree: %s\n' "$worktree"
else
	mkdir -p "$repo_root/.worktrees"
	git -C "$repo_root" worktree add --detach "$worktree" "$ref"
fi

timestamp() {
	date +%Y%m%d%H%M%S
}

link_file() {
	source=$1
	target=$2
	label=$3

	if [ ! -e "$source" ]; then
		printf 'Skipped %s: source not found: %s\n' "$label" "$source"
		return
	fi

	mkdir -p "$(dirname "$target")"
	if [ -L "$target" ]; then
		ln -sfn "$source" "$target"
	elif [ -e "$target" ]; then
		backup=$target.local-$(timestamp)
		mv "$target" "$backup"
		printf 'Backed up existing %s: %s\n' "$label" "$backup"
		ln -s "$source" "$target"
	else
		ln -s "$source" "$target"
	fi
	printf 'Linked %s: %s -> %s\n' "$label" "$target" "$source"
}

config_source=${WEFT_WORKTREE_CONFIG:-}
if [ -z "$config_source" ] && [ -f "$repo_root/.weft/config.toml" ]; then
	config_source=$repo_root/.weft/config.toml
fi
if [ -z "$config_source" ] && [ -n "${WEFT_HOME:-}" ] && [ -f "$WEFT_HOME/config.toml" ]; then
	config_source=$WEFT_HOME/config.toml
fi
if [ -z "$config_source" ] && [ -f "$HOME/.weft/config.toml" ]; then
	config_source=$HOME/.weft/config.toml
fi

link_file "$repo_root/.env" "$worktree/.env" ".env"
if [ -n "$config_source" ]; then
	link_file "$config_source" "$worktree/.weft/config.toml" "config.toml"
else
	printf 'Skipped config.toml: set WEFT_WORKTREE_CONFIG or create %s/.weft/config.toml or %s/.weft/config.toml.\n' "$repo_root" "$HOME"
fi

printf 'Ready: %s\n' "$worktree"
