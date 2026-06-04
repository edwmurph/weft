#!/usr/bin/env bash
set -euo pipefail

usage() {
	cat >&2 <<'USAGE'
Usage: cleanup-temp-supervisors.sh [--dry-run] [--yes] [--remove-runtime-dirs]

Finds Weft supervisor processes backed by temp runtime directories, such as old
integration-test runtimes under macOS /var/folders, and optionally stops them.

Dry-run is the default. The script preserves ~/.weft, the current repo checkout,
and registered .worktrees runtimes; use scripts/cleanup-worktrees.sh for those.

Options:
  --dry-run              print the cleanup plan without changing anything
  --yes                  stop targeted temp supervisors
  --remove-runtime-dirs  after a successful stop, remove temp .weft/.weft-runtime dirs
  -h, --help             show this help
USAGE
}

execute=false
remove_runtime_dirs=false

for arg in "$@"; do
	case "$arg" in
		--dry-run)
			execute=false
			;;
		--yes | -y)
			execute=true
			;;
		--remove-runtime-dirs)
			remove_runtime_dirs=true
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

repo_root=$(git rev-parse --show-toplevel 2>/dev/null || pwd -P)
home_runtime=${HOME:-}/.weft
registered_worktrees=()

load_registered_worktrees() {
	local record_path=

	while IFS= read -r line || [[ -n "$line" ]]; do
		if [[ -z "$line" ]]; then
			if [[ -n "$record_path" ]]; then
				registered_worktrees+=("$record_path")
			fi
			record_path=
			continue
		fi
		case "$line" in
			worktree\ *)
				record_path=${line#worktree }
				;;
		esac
	done < <(git -C "$repo_root" worktree list --porcelain 2>/dev/null || true)
	if [[ -n "$record_path" ]]; then
		registered_worktrees+=("$record_path")
	fi
}

is_weft_supervisor_command() {
	local command=$1
	local exe=
	local arg=
	local base=
	# shellcheck disable=SC2086
	set -- $command
	exe=${1:-}
	arg=${2:-}
	base=${exe##*/}
	case "$base" in
		weft | weft-* | weft_* | weft.*)
			[[ "$arg" == "_supervisor" ]]
			return
			;;
	esac
	return 1
}

runtime_dir_for_pid() {
	local pid=$1
	local path=
	if ! command -v lsof >/dev/null 2>&1; then
		return 1
	fi
	while IFS= read -r path; do
		path=${path#n}
		case "$path" in
			*/weftd.lock | */weftd.log)
				dirname "$path"
				return 0
				;;
		esac
	done < <(lsof -Fn -p "$pid" 2>/dev/null || true)
	return 1
}

is_temp_runtime_dir() {
	local runtime_dir=$1
	case "$runtime_dir" in
		/private/var/folders/* | /var/folders/* | /private/tmp/* | /tmp/*)
			return 0
			;;
	esac
	return 1
}

runtime_is_removable_dir() {
	local runtime_dir=$1
	case "$(basename "$runtime_dir")" in
		.weft | .weft-runtime)
			is_temp_runtime_dir "$runtime_dir"
			return
			;;
	esac
	return 1
}

runtime_under_registered_worktree() {
	local runtime_dir=$1
	local worktree=

	for worktree in "${registered_worktrees[@]}"; do
		[[ "$runtime_dir" == "$worktree/.weft" || "$runtime_dir" == "$worktree/.weft-runtime" ]] && return 0
	done
	return 1
}

classify_runtime() {
	local runtime_dir=$1
	if [[ -z "$runtime_dir" ]]; then
		printf 'keep:no runtime files found'
	elif [[ -n "$home_runtime" && "$runtime_dir" == "$home_runtime" ]]; then
		printf 'keep:installed ~/.weft runtime'
	elif [[ "$runtime_dir" == "$repo_root/.weft" || "$runtime_dir" == "$repo_root/.weft-runtime" ]]; then
		printf 'keep:primary checkout runtime'
	elif runtime_under_registered_worktree "$runtime_dir"; then
		printf 'keep:registered worktree runtime; use scripts/cleanup-worktrees.sh'
	elif [[ "$runtime_dir" == "$repo_root/.worktrees/"* ]]; then
		printf 'keep:unregistered .worktrees runtime; inspect separately'
	elif is_temp_runtime_dir "$runtime_dir"; then
		printf 'target:temp runtime'
	else
		printf 'keep:unknown non-temp runtime'
	fi
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

display_command() {
	local command=$1
	if [[ ${#command} -le 140 ]]; then
		printf '%s' "$command"
	else
		printf '%s...' "${command:0:137}"
	fi
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

targets=()
kept=()
total_target_kib=0
load_registered_worktrees

while IFS= read -r line || [[ -n "$line" ]]; do
	line=${line#"${line%%[![:space:]]*}"}
	[[ -z "$line" ]] && continue
	pid=${line%%[[:space:]]*}
	rest=${line#"$pid"}
	rest=${rest#"${rest%%[![:space:]]*}"}
	rss=${rest%%[[:space:]]*}
	command=${rest#"$rss"}
	command=${command#"${command%%[![:space:]]*}"}
	case "$pid" in
		'' | *[!0-9]*)
			continue
			;;
	esac
	case "$rss" in
		'' | *[!0-9]*)
			continue
			;;
	esac
	if ! is_weft_supervisor_command "$command"; then
		continue
	fi
	runtime_dir=$(runtime_dir_for_pid "$pid" || true)
	classification=$(classify_runtime "$runtime_dir")
	case "$classification" in
		target:*)
			targets+=("$pid|$rss|$runtime_dir|$command")
			total_target_kib=$((total_target_kib + rss))
			;;
		keep:*)
			kept+=("$pid|$rss|${classification#keep:}|$runtime_dir|$command")
			;;
	esac
done < <(ps -axo pid=,rss=,command=)

printf 'Temp Weft supervisors to stop (%d, %s RSS):\n' "${#targets[@]}" "$(format_kib "$total_target_kib")"
if [[ ${#targets[@]} -eq 0 ]]; then
	printf '  none\n'
else
	for item in "${targets[@]}"; do
		IFS='|' read -r pid rss runtime_dir command <<<"$item"
		printf '  - pid %s (%s RSS) runtime %s command %s\n' "$pid" "$(format_kib "$rss")" "$runtime_dir" "$(display_command "$command")"
	done
fi

printf '\nKept Weft supervisors (%d):\n' "${#kept[@]}"
if [[ ${#kept[@]} -eq 0 ]]; then
	printf '  none\n'
else
	for item in "${kept[@]}"; do
		IFS='|' read -r pid rss reason runtime_dir command <<<"$item"
		printf '  - pid %s (%s RSS) %s runtime %s command %s\n' "$pid" "$(format_kib "$rss")" "$reason" "${runtime_dir:-unknown}" "$(display_command "$command")"
	done
fi

if [[ "$execute" != true ]]; then
	printf '\nDry run only. No temp supervisors were stopped.\n'
	exit 0
fi

printf '\nStopping targeted temp supervisors.\n'
for item in "${targets[@]}"; do
	IFS='|' read -r pid _rss runtime_dir _command <<<"$item"
	if ! kill -0 "$pid" 2>/dev/null; then
		printf '  pid %s already stopped\n' "$pid"
		continue
	fi
	printf '  signaling pid %s\n' "$pid"
	kill -TERM "$pid" 2>/dev/null || true
	if wait_for_pid_exit "$pid"; then
		printf '  pid %s stopped\n' "$pid"
		if [[ "$remove_runtime_dirs" == true && -n "$runtime_dir" ]] && runtime_is_removable_dir "$runtime_dir"; then
			printf '  removing runtime dir %s\n' "$runtime_dir"
			rm -rf "$runtime_dir"
		fi
	else
		printf '  pid %s did not stop after SIGTERM\n' "$pid" >&2
	fi
done

printf 'Temp supervisor cleanup complete.\n'
