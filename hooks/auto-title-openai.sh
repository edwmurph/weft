#!/usr/bin/env bash
set -euo pipefail

env_file="${WEFT_OPENAI_ENV_FILE:-.env}"
if [[ -f "$env_file" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "$env_file"
  set +a
fi

if [[ -z "${OPENAI_API_KEY:-}" ]]; then
  echo "OPENAI_API_KEY is required" >&2
  exit 1
fi
if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required" >&2
  exit 1
fi
if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required" >&2
  exit 1
fi

payload="$(cat)"
model="${OPENAI_TITLE_MODEL:-gpt-5.4-nano}"
curl_retries="${OPENAI_TITLE_CURL_RETRIES:-2}"
curl_retry_delay_seconds="${OPENAI_TITLE_CURL_RETRY_DELAY_SECONDS:-1}"

first_message="$(jq -r '.first_message // ""' <<<"$payload")"
auto_title_columns="$(jq -r '(.auto_title_columns // .title_columns // 32) | tostring' <<<"$payload")"
if [[ -z "$auto_title_columns" || "$auto_title_columns" =~ [^0-9] ]]; then
  auto_title_columns=32
fi
if (( auto_title_columns < 1 )); then
  auto_title_columns=1
fi

request="$(
  jq -n \
    --arg model "$model" \
    --arg first_message "$first_message" \
    --arg auto_title_columns "$auto_title_columns" \
    '{
      model: $model,
      store: false,
      max_output_tokens: 64,
      reasoning: { effort: "low" },
      text: { verbosity: "low" },
      input: [
        {
          role: "system",
          content: ("Write a very short Weft task title for the first message sent to a coding agent. Output only the title. Use only the first message. Do not use repo, workdir, product, or app context unless it appears in the message. Fit within " + $auto_title_columns + " terminal display columns so the title fits after Weft reserves space for the task marker, widest configured task-type badge, and title-template fields such as status. Count ordinary ASCII characters as one column. Prefer 1 to 4 words. If the column limit is tiny, output the shortest useful label. If the message is a greeting or not a clear task, preserve its simple intent, often exactly. Examples: hi -> hi; hello -> hello; can you help? -> Help Request. If it is a task request, summarize the requested task compactly.")
        },
        {
          role: "user",
          content: $first_message
        }
      ]
    }'
)"

curl_args=(-fsS --retry "$curl_retries" --retry-delay "$curl_retry_delay_seconds")
if curl --help all 2>/dev/null | grep -q -- "--retry-all-errors"; then
  curl_args+=(--retry-all-errors)
fi

response="$(
  curl "${curl_args[@]}" https://api.openai.com/v1/responses \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer ${OPENAI_API_KEY}" \
    -d "$request"
)"

jq -r '
  [.output[]? | select(.type == "message") | .content[]? | select(.type == "output_text") | .text]
  | join("\n")
' <<<"$response" | sed -n '/[^[:space:]]/{s/^[[:space:]]*//;s/[[:space:]]*$//;p;q;}'
