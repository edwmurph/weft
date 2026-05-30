package release

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

var (
	explicitBumpRE = regexp.MustCompile(`(?im)^(?:semver[- ]bump|release[- ]bump):\s*(major|minor|patch)\b`)
	breakingRE     = regexp.MustCompile(`(?im)^(BREAKING CHANGE:|[a-z][a-z0-9_-]*(?:\([^)]*\))?!:)`)
	minorRE        = regexp.MustCompile(`(?im)^(feat(?:\([^)]*\))?:|add(?:ed|s)?\b|creat(?:e|ed|es)\b|implement(?:ed|s)?\b|introduc(?:e|ed|es)\b|support(?:ed|s)?\b)`)
	patchRE        = regexp.MustCompile(`(?im)^(fix|docs?|chore|test|tests|refactor|update|remove|repair)\b`)
	semverRE       = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$`)
	goVersionRE    = regexp.MustCompile(`(?m)^var Version = "([^"]+)"$`)
	conventionalRE = regexp.MustCompile(`^([a-z][a-z0-9-]*)(?:\([^)]*\))?(!)?:\s*(.+)$`)
	footerRE       = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9- ]*:\s+`)
	releaseNotesRE = regexp.MustCompile(`(?i)^Release-Notes:\s*(.*)$`)
	bulletRE       = regexp.MustCompile(`^(?:[-*]|\d+\.)\s+`)
	releaseMetaRE  = regexp.MustCompile(`(?i)^release v?\d+\.\d+\.\d+(?:\s|$)`)
)

const EmptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

type Commit struct {
	Hash    string
	Subject string
	Body    string
}

func InferBump(messages string, changedFiles []string) string {
	if match := explicitBumpRE.FindStringSubmatch(messages); len(match) > 1 {
		return strings.ToLower(match[1])
	}
	if breakingRE.MatchString(messages) {
		return "major"
	}
	if minorRE.MatchString(messages) {
		return "minor"
	}
	if patchRE.MatchString(messages) {
		return "patch"
	}
	for _, file := range changedFiles {
		if strings.HasPrefix(file, ".github/workflows/") {
			return "minor"
		}
	}
	return "patch"
}

func BumpVersion(version string, bump string) (string, error) {
	match := semverRE.FindStringSubmatch(version)
	if len(match) != 4 {
		return "", fmt.Errorf("unsupported version %q; expected MAJOR.MINOR.PATCH", version)
	}
	major, _ := strconv.Atoi(match[1])
	minor, _ := strconv.Atoi(match[2])
	patch, _ := strconv.Atoi(match[3])
	switch bump {
	case "major":
		return fmt.Sprintf("%d.0.0", major+1), nil
	case "minor":
		return fmt.Sprintf("%d.%d.0", major, minor+1), nil
	case "patch":
		return fmt.Sprintf("%d.%d.%d", major, minor, patch+1), nil
	default:
		return "", fmt.Errorf("unsupported bump %q", bump)
	}
}

func ReadVersion(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func WriteVersion(path string, version string) error {
	return os.WriteFile(path, []byte(version+"\n"), 0o644)
}

func WriteGoVersion(path string, version string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	updated := goVersionRE.ReplaceAllString(string(data), `var Version = "`+version+`"`)
	if updated == string(data) {
		return fmt.Errorf("could not update Go version in %s", path)
	}
	return os.WriteFile(path, []byte(updated), 0o644)
}

func ChangedFiles(base string, head string) ([]string, error) {
	out, err := git("diff", "--name-only", "--diff-filter=ACMRT", base, head)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) != "" {
			files = append(files, strings.TrimSpace(line))
		}
	}
	return files, nil
}

func CommitMessages(base string, head string) (string, error) {
	if base == EmptyTree {
		return git("log", "--format=%B%n---END-COMMIT---", head)
	}
	return git("log", "--format=%B%n---END-COMMIT---", base+".."+head)
}

func Commits(base string, head string) ([]Commit, error) {
	args := []string{"log", "--reverse", "--format=%H%x00%B%x1e"}
	if base == EmptyTree {
		args = append(args, head)
	} else {
		args = append(args, base+".."+head)
	}
	out, err := git(args...)
	if err != nil {
		return nil, err
	}
	var commits []Commit
	for _, record := range strings.Split(out, "\x1e") {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}
		fields := strings.SplitN(record, "\x00", 2)
		if len(fields) != 2 {
			return nil, fmt.Errorf("could not parse git log record %q", record)
		}
		subject, body := splitCommitMessage(fields[1])
		commits = append(commits, Commit{
			Hash:    strings.TrimSpace(fields[0]),
			Subject: subject,
			Body:    body,
		})
	}
	return commits, nil
}

func ReleaseNotes(base string, head string) (string, error) {
	commits, err := Commits(base, head)
	if err != nil {
		return "", err
	}
	return RenderReleaseNotes(commits), nil
}

func RenderReleaseNotes(commits []Commit) string {
	sections := map[string][]string{}
	for _, commit := range commits {
		if ignoreReleaseNoteCommit(commit) {
			continue
		}
		details := parseConventionalSubject(commit.Subject)
		notes := explicitReleaseNotes(commit.Body)
		if len(notes) == 0 {
			notes = []string{details.Description}
		}
		section := releaseNoteSection(details)
		if details.Breaking || breakingRE.MatchString(commit.Body) {
			section = "breaking"
		}
		for _, note := range notes {
			normalized := normalizeReleaseNote(note)
			if normalized == "" {
				continue
			}
			sections[section] = append(sections[section], normalized)
		}
	}

	order := []struct {
		key   string
		title string
	}{
		{key: "breaking", title: "Breaking Changes"},
		{key: "features", title: "Features"},
		{key: "fixes", title: "Fixes"},
		{key: "documentation", title: "Documentation"},
		{key: "maintenance", title: "Maintenance"},
	}
	var builder strings.Builder
	for _, section := range order {
		notes := sections[section.key]
		if len(notes) == 0 {
			continue
		}
		if builder.Len() > 0 {
			builder.WriteString("\n")
		}
		builder.WriteString("## ")
		builder.WriteString(section.title)
		builder.WriteString("\n\n")
		for _, note := range notes {
			builder.WriteString("- ")
			builder.WriteString(note)
			builder.WriteString("\n")
		}
	}
	if builder.Len() == 0 {
		return "## Maintenance\n\n- No user-facing changes.\n"
	}
	return builder.String()
}

func UsableBase(base string, head string) string {
	if base == "" || strings.Trim(base, "0") == "" {
		return FallbackBase(head)
	}
	if _, err := git("cat-file", "-e", base+"^{commit}"); err != nil {
		return FallbackBase(head)
	}
	return base
}

func FallbackBase(head string) string {
	if out, err := git("describe", "--tags", "--match", "v[0-9]*", "--abbrev=0", head); err == nil && strings.TrimSpace(out) != "" {
		return strings.TrimSpace(out)
	}
	if out, err := git("rev-parse", head+"^"); err == nil && strings.TrimSpace(out) != "" {
		return strings.TrimSpace(out)
	}
	return EmptyTree
}

func RenderFormula(formulaName string, versionURL string, sha256 string) string {
	className := ""
	for _, part := range strings.Split(strings.ReplaceAll(formulaName, "_", "-"), "-") {
		if part == "" {
			continue
		}
		className += strings.ToUpper(part[:1]) + part[1:]
	}
	return fmt.Sprintf(`# Generated by edwmurph/weft .github/workflows/publish-homebrew.yml.
class %s < Formula
  desc "Coordinate multiple Codex sessions from one supervisor-backed TUI"
  homepage "https://github.com/edwmurph/weft"
  url "%s"
  sha256 "%s"
  license "MIT"

  depends_on "go" => :build

  def install
    system "go", "build", "-ldflags", "-X github.com/edwmurph/weft/internal/version.Version=#{version}", "-o", bin/"%s", "./cmd/weft"
  end

  test do
    ENV["WEFT_HOME"] = testpath/"weft-home"
    ENV["WEFT_WORKDIR"] = testpath
    assert_match "Supervisor-backed Codex command center", shell_output("#{bin}/%s --help")
    assert_match "supervisor owns Codex PTYs", shell_output("#{bin}/%s doctor")
  end
end
`, className, versionURL, sha256, formulaName, formulaName, formulaName)
}

type conventionalSubject struct {
	Type        string
	Description string
	Breaking    bool
}

func splitCommitMessage(message string) (string, string) {
	message = strings.TrimSpace(message)
	subject, body, found := strings.Cut(message, "\n")
	if !found {
		return strings.TrimSpace(subject), ""
	}
	return strings.TrimSpace(subject), strings.TrimSpace(body)
}

func parseConventionalSubject(subject string) conventionalSubject {
	subject = strings.TrimSpace(subject)
	match := conventionalRE.FindStringSubmatch(subject)
	if len(match) == 0 {
		return conventionalSubject{Description: subject}
	}
	return conventionalSubject{
		Type:        strings.ToLower(match[1]),
		Breaking:    match[2] == "!",
		Description: strings.TrimSpace(match[3]),
	}
}

func releaseNoteSection(subject conventionalSubject) string {
	switch subject.Type {
	case "feat":
		return "features"
	case "fix":
		return "fixes"
	case "docs":
		return "documentation"
	default:
		return "maintenance"
	}
}

func explicitReleaseNotes(body string) []string {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	var notes []string
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		match := releaseNotesRE.FindStringSubmatch(line)
		if len(match) == 0 {
			continue
		}
		sectionStart := len(notes)
		if inline := stripBullet(match[1]); inline != "" {
			notes = append(notes, inline)
		}
		for j := i + 1; j < len(lines); j++ {
			next := strings.TrimSpace(lines[j])
			if next == "" {
				if len(notes) > sectionStart {
					i = j
					break
				}
				continue
			}
			if releaseNotesRE.MatchString(next) {
				i = j - 1
				break
			}
			if footerRE.MatchString(next) && !bulletRE.MatchString(next) {
				i = j - 1
				break
			}
			notes = append(notes, stripBullet(next))
			i = j
		}
	}
	return notes
}

func normalizeReleaseNote(note string) string {
	note = stripConventionalPrefix(stripBullet(note))
	if note == "" {
		return ""
	}
	if note[0] >= 'a' && note[0] <= 'z' {
		note = string(note[0]-'a'+'A') + note[1:]
	}
	return note
}

func stripConventionalPrefix(note string) string {
	note = strings.TrimSpace(note)
	match := conventionalRE.FindStringSubmatch(note)
	if len(match) == 0 {
		return note
	}
	return strings.TrimSpace(match[3])
}

func stripBullet(note string) string {
	return strings.TrimSpace(bulletRE.ReplaceAllString(strings.TrimSpace(note), ""))
}

func ignoreReleaseNoteCommit(commit Commit) bool {
	message := strings.ToLower(commit.Subject + "\n" + commit.Body)
	return strings.Contains(message, "[skip ci]") ||
		strings.Contains(message, "[ci skip]") ||
		releaseMetaRE.MatchString(strings.TrimSpace(commit.Subject))
}

func git(args ...string) (string, error) {
	out, err := exec.Command("git", args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
