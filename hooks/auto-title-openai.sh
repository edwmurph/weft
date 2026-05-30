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

first_message="$(jq -r '.first_message // ""' <<<"$payload")"

request="$(
  jq -n \
    --arg model "$model" \
    --arg first_message "$first_message" \
    '{
      model: $model,
      store: false,
      max_output_tokens: 64,
      reasoning: { effort: "low" },
      text: { verbosity: "low" },
      input: [
        {
          role: "system",
          content: "Write a very short terminal tab title for the first message sent to a coding agent. Output only the title. Use only the first message. Do not use repo, workdir, product, or app context unless it appears in the message. If the message is a greeting or not a clear task, preserve its simple intent, often exactly. Examples: hi -> hi; hello -> hello; can you help? -> Help Request. If it is a task request, summarize the requested task in 1 to 5 words."
        },
        {
          role: "user",
          content: $first_message
        }
      ]
    }'
)"

response="$(
  curl -fsS https://api.openai.com/v1/responses \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer ${OPENAI_API_KEY}" \
    -d "$request"
)"

jq -r '
  [.output[]? | select(.type == "message") | .content[]? | select(.type == "output_text") | .text]
  | join("\n")
' <<<"$response" | sed -n '/[^[:space:]]/{s/^[[:space:]]*//;s/[[:space:]]*$//;p;q;}'
