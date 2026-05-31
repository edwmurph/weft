#!/usr/bin/env bash
set -euo pipefail

usage() {
	cat >&2 <<'USAGE'
Usage: scripts/cleanup-worktrees.sh [--yes] [--dry-run] [--skip-supervisors] [--skip-prune]

Stops Weft supervisors for every Git-registered auxiliary worktree under
./.worktrees/, removes those worktrees, and prunes stale Git worktree metadata.

Options:
  --yes               run without the confirmation prompt
  --dry-run           print the cleanup plan without changing anything
  --skip-supervisors  remove worktrees without trying to stop WEFT_ROOT supervisors
  --skip-prune        leave stale Git worktree metadata untouched
  -h, --help          show this help
USAGE
}

confirm=false
dry_run=false
skip_supervisors=false
skip_prune=false

for arg in "$@"; do
	case "$arg" in
		--yes | -y)
			confirm=true
			;;
		--dry-run)
			dry_run=true
			;;
		--skip-supervisors)
			skip_supervisors=true
			;;
		--skip-prune)
			skip_prune=true
			;;
		-h | --help)
			usage
			exit 0
			;;
		*)
			printf 'Unknown option: %s\n\n' "$arg" >&2
			usage
			exit 2
			;;
	esac
done

git_common_dir=$(git rev-parse --path-format=absolute --git-common-dir)
repo_root=$(cd "$git_common_dir/.." && pwd -P)
worktree_root=$repo_root/.worktrees

targets=()
stale_targets=()
kept_external=()
record_path=
record_prunable=false

flush_record() {
	if [[ -z "$record_path" ]]; then
		return
	fi

	if [[ "$record_path" == "$repo_root" ]]; then
		:
	elif [[ "$record_path" == "$worktree_root"/* ]]; then
		if [[ -d "$record_path" && "$record_prunable" != true ]]; then
			targets+=("$record_path")
		else
			stale_targets+=("$record_path")
		fi
	else
		kept_external+=("$record_path")
	fi

	record_path=
	record_prunable=false
}

while IFS= read -r line || [[ -n "$line" ]]; do
	if [[ -z "$line" ]]; then
		flush_record
		continue
	fi
	case "$line" in
		worktree\ *)
			record_path=${line#worktree }
			;;
		prunable*)
			record_prunable=true
			;;
	esac
done < <(git -C "$repo_root" worktree list --porcelain)
flush_record

display_path() {
	local path=$1
	if [[ "$path" == "$repo_root" ]]; then
		printf '.'
	elif [[ "$path" == "$repo_root/"* ]]; then
		printf '%s' "${path#"$repo_root/"}"
	else
		printf '%s' "$path"
	fi
}

du_kib() {
	local path=$1
	if [[ ! -e "$path" ]]; then
		printf '0\n'
		return
	fi
	du -sk "$path" 2>/dev/null | awk 'NR == 1 { print $1 }'
}

format_kib() {
	local kib=$1
	awk -v kib="$kib" 'BEGIN {
		if (kib >= 1048576) {
			printf "%.1f GiB", kib / 1048576
		} else if (kib >= 1024) {
			printf "%.1f MiB", kib / 1024
		} else {
			printf "%d KiB", kib
		}
	}'
}

pid_from_file() {
	local pid_file=$1
	local pid=
	if [[ ! -f "$pid_file" ]]; then
		return 1
	fi
	IFS= read -r pid <"$pid_file" || true
	pid=${pid//[[:space:]]/}
	case "$pid" in
		'' | *[!0-9]*)
			return 1
			;;
	esac
	printf '%s\n' "$pid"
}

process_looks_like_weft() {
	local pid=$1
	local details=
	details=$(ps -p "$pid" -o comm= -o args= 2>/dev/null || true)
	case "$details" in
		*weft* | *weftd*)
			return 0
			;;
	esac
	return 1
}

runtime_status() {
	local worktree=$1
	local runtime_dir=$worktree/.weft
	local pid_file=$runtime_dir/weftd.pid
	local pid=

	if pid=$(pid_from_file "$pid_file") && kill -0 "$pid" 2>/dev/null && process_looks_like_weft "$pid"; then
		printf 'running supervisor pid %s' "$pid"
	elif [[ -f "$pid_file" ]]; then
		printf 'stale supervisor pid file'
	elif [[ -S "$runtime_dir/weft.sock" ]]; then
		printf 'supervisor socket present'
	elif [[ -d "$runtime_dir" ]]; then
		printf 'runtime dir present'
	else
		printf 'no runtime dir'
	fi
}

run_weft_close() {
	local worktree=$1
	local status=127

	if command -v weft >/dev/null 2>&1; then
		status=0
		WEFT_ROOT="$worktree" weft close --kill --yes || status=$?
		if [[ "$status" -eq 0 ]]; then
			return 0
		fi
		printf '  installed weft close command failed; trying source command if available\n' >&2
	fi
	if command -v go >/dev/null 2>&1 && [[ -d "$repo_root/cmd/weft" ]]; then
		WEFT_ROOT="$worktree" go -C "$repo_root" run ./cmd/weft close --kill --yes
		return $?
	fi
	return "$status"
}

wait_for_pid_exit() {
	local pid=$1
	local _i=
	for _i in {1..40}; do
		if ! kill -0 "$pid" 2>/dev/null; then
			return 0
		fi
		sleep 0.1
	done
	return 1
}

terminate_supervisor_pid() {
	local worktree=$1
	local runtime_dir=$worktree/.weft
	local pid_file=$runtime_dir/weftd.pid
	local pid=

	if ! pid=$(pid_from_file "$pid_file"); then
		return 0
	fi
	if ! kill -0 "$pid" 2>/dev/null; then
		return 0
	fi
	if ! process_looks_like_weft "$pid"; then
		printf 'Refusing to signal pid %s from %s because it does not look like Weft.\n' "$pid" "$(display_path "$pid_file")" >&2
		return 1
	fi

	printf '  signaling supervisor pid %s\n' "$pid"
	kill -TERM "$pid" 2>/dev/null || true
	if wait_for_pid_exit "$pid"; then
		return 0
	fi

	printf 'Supervisor pid %s did not stop after SIGTERM.\n' "$pid" >&2
	return 1
}

supervisor_may_exist() {
	local worktree=$1
	local runtime_dir=$worktree/.weft
	[[ -f "$runtime_dir/weftd.pid" || -S "$runtime_dir/weft.sock" || -f "$runtime_dir/weftd.lock" ]]
}

stop_supervisor() {
	local worktree=$1
	local runtime_dir=$worktree/.weft
	local pid_file=$runtime_dir/weftd.pid
	local pid=

	if [[ "$skip_supervisors" == true ]]; then
		return 0
	fi
	if ! supervisor_may_exist "$worktree"; then
		return 0
	fi

	printf 'Stopping Weft supervisor for %s\n' "$(display_path "$worktree")"
	if run_weft_close "$worktree"; then
		if pid=$(pid_from_file "$pid_file") && kill -0 "$pid" 2>/dev/null && process_looks_like_weft "$pid"; then
			printf '  supervisor pid %s is still running after close; falling back to SIGTERM\n' "$pid"
			terminate_supervisor_pid "$worktree"
		fi
		return 0
	fi

	printf '  weft close --kill failed; falling back to SIGTERM when a valid pid exists\n' >&2
	terminate_supervisor_pid "$worktree"
}

print_plan() {
	local total_kib=0
	local target=
	local size=

	printf 'Primary checkout preserved: %s\n' "$repo_root"
	printf 'Target root: %s\n' "$worktree_root"
	printf '\n'

	if [[ ${#targets[@]} -eq 0 ]]; then
		printf 'No registered auxiliary worktrees under .worktrees/ are targeted.\n'
	else
		printf 'Worktrees to remove (%d):\n' "${#targets[@]}"
		for target in "${targets[@]}"; do
			size=$(du_kib "$target")
			total_kib=$((total_kib + size))
			printf '  - %s (%s, %s)\n' "$(display_path "$target")" "$(format_kib "$size")" "$(runtime_status "$target")"
		done
		printf 'Estimated worktree storage to free: %s\n' "$(format_kib "$total_kib")"
	fi

	if [[ ${#stale_targets[@]} -gt 0 ]]; then
		printf '\nStale .worktrees/ Git metadata to prune (%d):\n' "${#stale_targets[@]}"
		for target in "${stale_targets[@]}"; do
			printf '  - %s\n' "$(display_path "$target")"
		done
	fi

	if [[ ${#kept_external[@]} -gt 0 ]]; then
		printf '\nKeeping %d registered worktree(s) outside %s.\n' "${#kept_external[@]}" "$worktree_root"
	fi
}

print_plan

if [[ "$dry_run" == true ]]; then
	printf '\nDry run only. No worktrees were removed.\n'
	exit 0
fi

if [[ ${#targets[@]} -eq 0 && ( ${#stale_targets[@]} -eq 0 || "$skip_prune" == true ) ]]; then
	printf '\nNothing to clean.\n'
	exit 0
fi

if [[ "$confirm" != true ]]; then
	printf '\nThis assumes every targeted worktree is disposable, including uncommitted changes.\n'
	read -r -p 'Delete targeted worktrees and clean their supervisors? [y/N] ' answer
	case "$answer" in
		y | Y | yes | YES)
			;;
		*)
			printf 'Cleanup canceled.\n'
			exit 0
			;;
	esac
fi

for target in "${targets[@]}"; do
	stop_supervisor "$target"
	printf 'Removing worktree %s\n' "$(display_path "$target")"
	git -C "$repo_root" worktree remove --force "$target"
done

if [[ "$skip_prune" != true ]]; then
	printf 'Pruning stale Git worktree metadata\n'
	git -C "$repo_root" worktree prune
fi

printf 'Worktree cleanup complete.\n'
