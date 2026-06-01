package hooks_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAutoTitleOpenAIRetriesCurlReceiveFailures(t *testing.T) {
	binDir := t.TempDir()
	attemptsPath := filepath.Join(t.TempDir(), "curl-attempts")
	writeExecutable(t, filepath.Join(binDir, "curl"), `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--help" ]]; then
  echo "--retry-all-errors"
  exit 0
fi
retry=0
while (($# > 0)); do
  if [[ "$1" == "--retry" ]]; then
    shift
    retry="${1:-0}"
  fi
  shift || true
done
max_attempts=$((retry + 1))
for ((try = 1; try <= max_attempts; try++)); do
  attempt=0
  if [[ -f "$WEFT_TEST_CURL_ATTEMPTS" ]]; then
    attempt="$(cat "$WEFT_TEST_CURL_ATTEMPTS")"
  fi
  attempt=$((attempt + 1))
  printf '%s' "$attempt" > "$WEFT_TEST_CURL_ATTEMPTS"
  if (( attempt < 3 )); then
    echo "curl: (56) Recv failure: reset by peer" >&2
    continue
  fi
  printf '%s\n' '{"output":[{"type":"message","content":[{"type":"output_text","text":"Retried title"}]}]}'
  exit 0
done
exit 56
`)
	writeExecutable(t, filepath.Join(binDir, "jq"), `#!/usr/bin/env bash
set -euo pipefail
args="$*"
if [[ "$args" == *"-n"* ]]; then
  echo '{"request":true}'
  exit 0
fi
if [[ "$args" == *"first_message"* ]]; then
  echo "fix login"
  exit 0
fi
if [[ "$args" == *"auto_title_columns"* ]]; then
  echo "32"
  exit 0
fi
if [[ "$args" == *"output_text"* ]]; then
  echo "Retried title"
  exit 0
fi
echo ""
`)

	cmd := exec.Command("bash", "./auto-title-openai.sh")
	cmd.Stdin = strings.NewReader(`{"first_message":"fix login","auto_title_columns":32}`)
	cmd.Env = append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"OPENAI_API_KEY=test",
		"OPENAI_TITLE_CURL_RETRIES=2",
		"OPENAI_TITLE_CURL_RETRY_DELAY_SECONDS=0",
		"WEFT_TEST_CURL_ATTEMPTS="+attemptsPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hook failed: %v\n%s", err, output)
	}
	lines := strings.FieldsFunc(strings.TrimSpace(string(output)), func(r rune) bool { return r == '\n' || r == '\r' })
	if len(lines) == 0 || lines[len(lines)-1] != "Retried title" {
		t.Fatalf("hook output = %q", output)
	}
	raw, err := os.ReadFile(attemptsPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(raw)); got != "3" {
		t.Fatalf("curl attempts = %q, want 3", got)
	}
}

func writeExecutable(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
}
