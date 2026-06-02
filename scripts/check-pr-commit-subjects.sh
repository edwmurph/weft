#!/usr/bin/env bash
set -euo pipefail

base="${1:-}"
head="${2:-HEAD}"

if [[ -z "$base" ]]; then
  echo "usage: $0 <base-ref> [head-ref]" >&2
  exit 2
fi

if ! git rev-parse --verify "$base^{commit}" >/dev/null 2>&1; then
  echo "Base ref is not a commit: $base" >&2
  exit 2
fi

if ! git rev-parse --verify "$head^{commit}" >/dev/null 2>&1; then
  echo "Head ref is not a commit: $head" >&2
  exit 2
fi

subject_re='^(feat|fix|docs|refactor|chore)(\([^()]+\))?(!)?: [^[:space:]](.*[^[:space:]])?$'
invalid=0
count=0

while IFS=$'\t' read -r hash subject; do
  if [[ -z "$hash" ]]; then
    continue
  fi
  count=$((count + 1))
  if [[ "$subject" =~ $subject_re ]]; then
    continue
  fi
  short_hash="$(git rev-parse --short "$hash")"
  echo "::error title=Invalid PR commit subject::${short_hash} ${subject}"
  invalid=$((invalid + 1))
done < <(git log --reverse --format='%H%x09%s' "$base..$head")

if (( invalid > 0 )); then
  cat >&2 <<'EOF'

PR commit subjects must use Weft's release-note convention:

  feat: add a user-facing capability
  fix(tui): repair a user-visible bug
  docs: clarify installation steps
  refactor: simplify task state handling
  chore: update release automation

Allowed types: feat, fix, docs, refactor, chore.
Scopes are optional. Breaking markers are allowed, for example: feat!: remove legacy config aliases.
EOF
  exit 1
fi

echo "Validated ${count} PR commit subject(s)."
