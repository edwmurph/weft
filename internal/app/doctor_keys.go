package app

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/term"
)

var errKeyDoctorCanceled = errors.New("key doctor canceled")

const (
	iTerm2OptionBackspaceKey  = "0x7f-0x80000-0x33"
	iTerm2OptionEscValue      = "2"
	iTerm2OptionBackspaceText = "0x1b 0x7f"
)

var errPlistKeyMissing = errors.New("plist key missing")

type keyDoctorSample struct {
	Name  string
	Bytes []byte
	Label string
}

type keyDoctorTerminal struct {
	Kind string
	Name string
}

func doctorKeys(input *os.File, output io.Writer) error {
	if !term.IsTerminal(input.Fd()) {
		return errors.New("weft doctor keys requires an interactive terminal")
	}
	detected := detectDoctorTerminal(os.Environ())
	fmt.Fprintln(output, "Weft key doctor")
	if detected.Name != "" {
		fmt.Fprintf(output, "Detected terminal: %s\n", detected.Name)
	}
	fmt.Fprintln(output, "Press each requested key. Press Ctrl+C to cancel.")
	fmt.Fprintln(output)

	prompts := []string{"Backspace", "Option+Backspace", "Ctrl+Backspace"}
	samples := make([]keyDoctorSample, 0, len(prompts))
	for _, prompt := range prompts {
		sample, err := captureDoctorKey(input, output, prompt)
		if err != nil {
			return err
		}
		samples = append(samples, sample)
	}

	fmt.Fprintln(output)
	fmt.Fprint(output, keyDoctorReport(samples))
	return offerDoctorKeyFix(input, output, samples, detected, os.Environ())
}

func detectDoctorTerminal(env []string) keyDoctorTerminal {
	termProgram := envValue(env, "TERM_PROGRAM")
	switch {
	case strings.EqualFold(termProgram, "iTerm.app") || envValue(env, "ITERM_SESSION_ID") != "":
		return keyDoctorTerminal{Kind: "iterm2", Name: "iTerm2"}
	case strings.EqualFold(termProgram, "Apple_Terminal"):
		return keyDoctorTerminal{Kind: "apple_terminal", Name: "Terminal.app"}
	case strings.EqualFold(termProgram, "WezTerm") || envValue(env, "WEZTERM_EXECUTABLE") != "":
		return keyDoctorTerminal{Kind: "wezterm", Name: "WezTerm"}
	case strings.Contains(strings.ToLower(termProgram), "ghostty") || envValue(env, "GHOSTTY_RESOURCES_DIR") != "":
		return keyDoctorTerminal{Kind: "ghostty", Name: "Ghostty"}
	case termProgram != "":
		return keyDoctorTerminal{Kind: "unknown", Name: termProgram}
	default:
		return keyDoctorTerminal{Kind: "unknown", Name: "unknown"}
	}
}

func offerDoctorKeyFix(input *os.File, output io.Writer, samples []keyDoctorSample, detected keyDoctorTerminal, env []string) error {
	if !keyDoctorNeedsOptionBackspaceFix(samples) {
		return nil
	}
	fmt.Fprintln(output)
	switch detected.Kind {
	case "iterm2":
		if runtime.GOOS != "darwin" {
			fmt.Fprintln(output, "Detected iTerm2, but automatic profile updates are only supported on macOS.")
			return nil
		}
		fmt.Fprintln(output, "Detected iTerm2. Weft can set Left/Right Option Key to Esc+ for the current/default iTerm2 profile.")
		fmt.Fprintln(output, "It will also add an Option+Backspace fallback mapping, write a backup first, and may require a new iTerm2 tab or window.")
		inspection, inspectErr := inspectIterm2OptionBackspaceFix(env)
		if inspectErr == nil {
			fmt.Fprintln(output, inspection.Summary())
			if inspection.Configured {
				fmt.Fprintln(output, iterm2ConfiguredButCurrentSessionStaleMessage(inspection))
				return nil
			}
		} else if target := iTerm2FixTargetSummary(env); target != "" {
			fmt.Fprintln(output, target)
		}
		if !confirmWithIO(input, output, "Apply this iTerm2 key fix now? [y/N] ") {
			fmt.Fprintln(output, "No terminal settings changed.")
			return nil
		}
		message, err := applyIterm2OptionBackspaceFix(env)
		if err != nil {
			return err
		}
		fmt.Fprintln(output, message)
	case "apple_terminal":
		fmt.Fprintln(output, "Detected Terminal.app. Automatic Terminal.app profile editing is not supported yet.")
		fmt.Fprintln(output, "Open Terminal > Settings > Profiles > Keyboard and enable \"Use Option as Meta key\".")
	default:
		fmt.Fprintf(output, "Detected %s. Weft does not have an automatic fix for this terminal yet.\n", detected.Name)
	}
	return nil
}

func keyDoctorNeedsOptionBackspaceFix(samples []keyDoctorSample) bool {
	backspace, hasBackspace := keyDoctorSampleByName(samples, "Backspace")
	optionBackspace, hasOptionBackspace := keyDoctorSampleByName(samples, "Option+Backspace")
	return hasBackspace && hasOptionBackspace && bytes.Equal(backspace.Bytes, optionBackspace.Bytes)
}

func confirmWithIO(input io.Reader, output io.Writer, prompt string) bool {
	fmt.Fprint(output, prompt)
	answer, _ := bufio.NewReader(input).ReadString('\n')
	return strings.EqualFold(strings.TrimSpace(answer), "y")
}

func captureDoctorKey(input *os.File, output io.Writer, name string) (keyDoctorSample, error) {
	fmt.Fprintf(output, "Press %s: ", name)
	seq, err := readDoctorKeySequence(input)
	fmt.Fprintln(output)
	if err != nil {
		return keyDoctorSample{}, err
	}
	if bytes.Equal(seq, []byte{0x03}) {
		return keyDoctorSample{}, errKeyDoctorCanceled
	}
	return keyDoctorSample{Name: name, Bytes: seq, Label: describeDoctorKeySequence(seq)}, nil
}

func readDoctorKeySequence(input *os.File) ([]byte, error) {
	fd := int(input.Fd())
	oldState, err := term.MakeRaw(input.Fd())
	if err != nil {
		return nil, err
	}
	defer term.Restore(input.Fd(), oldState)

	buf := make([]byte, 64)
	n, err := input.Read(buf)
	if err != nil {
		return nil, err
	}
	seq := append([]byte{}, buf[:n]...)

	if err := syscall.SetNonblock(fd, true); err != nil {
		return seq, nil
	}
	defer syscall.SetNonblock(fd, false)

	deadline := time.Now().Add(35 * time.Millisecond)
	for time.Now().Before(deadline) && len(seq) < 64 {
		n, err := input.Read(buf)
		if n > 0 {
			seq = append(seq, buf[:n]...)
			deadline = time.Now().Add(10 * time.Millisecond)
			continue
		}
		if err != nil && !errors.Is(err, syscall.EAGAIN) && !errors.Is(err, syscall.EWOULDBLOCK) {
			return seq, nil
		}
		time.Sleep(2 * time.Millisecond)
	}
	return seq, nil
}

func keyDoctorReport(samples []keyDoctorSample) string {
	var out strings.Builder
	for _, sample := range samples {
		fmt.Fprintf(&out, "%-18s %-16s bytes: %s\n", sample.Name+":", sample.Label, doctorKeyBytes(sample.Bytes))
	}
	if advice := keyDoctorAdvice(samples); advice != "" {
		out.WriteString("\n")
		out.WriteString(advice)
	}
	return out.String()
}

func keyDoctorAdvice(samples []keyDoctorSample) string {
	optionBackspace, hasOptionBackspace := keyDoctorSampleByName(samples, "Option+Backspace")
	if keyDoctorNeedsOptionBackspaceFix(samples) {
		return strings.Join([]string{
			"Issue: Option+Backspace is indistinguishable from Backspace.",
			"Fix: configure your terminal to send Option as Meta/Esc.",
			"Terminal.app: enable \"Use Option as Meta key\" for the profile.",
			"iTerm2: set Left/Right Option Key to Esc+, or map Option+Backspace to Esc then DEL.",
			"For custom mappings, send bytes: 1b 7f.",
		}, "\n") + "\n"
	}
	if hasOptionBackspace && strings.HasPrefix(optionBackspace.Label, "alt+") {
		return "OK: Option+Backspace is distinguishable. Weft can handle it and forward it into the Task Console.\n"
	}
	return "Info: Option+Backspace is distinguishable, but it is not a standard Meta/Esc Backspace sequence.\n"
}

func keyDoctorSampleByName(samples []keyDoctorSample, name string) (keyDoctorSample, bool) {
	for _, sample := range samples {
		if sample.Name == name {
			return sample, true
		}
	}
	return keyDoctorSample{}, false
}

func describeDoctorKeySequence(seq []byte) string {
	switch {
	case bytes.Equal(seq, []byte{0x7f}):
		return "backspace"
	case bytes.Equal(seq, []byte{0x08}):
		return "ctrl+h"
	case bytes.Equal(seq, []byte{0x03}):
		return "ctrl+c"
	case bytes.Equal(seq, []byte{'\r'}):
		return "enter"
	case bytes.Equal(seq, []byte{'\t'}):
		return "tab"
	case bytes.Equal(seq, []byte("\x1b[3~")):
		return "delete"
	case bytes.Equal(seq, []byte("\x1b\x7f")):
		return "alt+backspace"
	case bytes.Equal(seq, []byte("\x1b\b")):
		return "alt+ctrl+h"
	case bytes.Equal(seq, []byte("\x1b\x1b[3~")) || bytes.Equal(seq, []byte("\x1b[3;3~")):
		return "alt+delete"
	case len(seq) > 1 && seq[0] == 0x1b:
		return "alt+" + describeDoctorKeySequence(seq[1:])
	case len(seq) > 0:
		if value, ok := printableDoctorKey(seq); ok {
			return value
		}
	}
	return "unknown"
}

func printableDoctorKey(seq []byte) (string, bool) {
	if !utf8.Valid(seq) {
		return "", false
	}
	value := string(seq)
	runes := []rune(value)
	if len(runes) != 1 {
		return "", false
	}
	if unicode.IsControl(runes[0]) {
		return "", false
	}
	return value, true
}

func doctorKeyBytes(seq []byte) string {
	if len(seq) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(seq))
	for _, b := range seq {
		parts = append(parts, fmt.Sprintf("%02x", b))
	}
	return strings.Join(parts, " ")
}

func applyIterm2OptionBackspaceFix(env []string) (string, error) {
	path, err := iterm2PreferencesPath(env)
	if err != nil {
		return "", iterm2FixError("locate preferences", "", envValue(env, "ITERM_PROFILE"), err)
	}
	customPreferences := iterm2UsingCustomPreferences(path)
	target, err := selectIterm2ProfileTarget(path, envValue(env, "ITERM_PROFILE"))
	if err != nil {
		return "", iterm2FixError("select profile", path, envValue(env, "ITERM_PROFILE"), err)
	}
	configured, err := iterm2OptionBackspaceFixConfigured(path, target.Index)
	if err != nil {
		return "", iterm2FixError("read key settings", path, target.Name, err)
	}
	if configured {
		return fmt.Sprintf("iTerm2 profile %q already sets Left/Right Option Key to Esc+ and maps Option+Backspace to Esc DEL.", target.Name), nil
	}
	backupPath, err := backupFile(path)
	if err != nil {
		return "", iterm2FixError("write backup", path, target.Name, err)
	}
	if err := updateIterm2OptionBackspaceMappingFile(path, target.Index); err != nil {
		return "", iterm2FixError("write updated preferences", path, target.Name, err)
	}
	lines := []string{
		fmt.Sprintf("Updated iTerm2 profile %q.", target.Name),
		"Backup: " + backupPath,
	}
	if customPreferences {
		lines = append(lines, "iTerm2 is loading settings from a custom folder; quit and reopen iTerm2, then rerun `weft doctor keys`.")
	} else {
		lines = append(lines, "Open a new iTerm2 tab or window, then rerun `weft doctor keys`.")
	}
	return strings.Join(lines, "\n"), nil
}

type iterm2FixInspection struct {
	Path              string
	Profile           string
	LeftOptionSends   string
	RightOptionSends  string
	MappingConfigured bool
	CustomPreferences bool
	Configured        bool
}

func (i iterm2FixInspection) Summary() string {
	return fmt.Sprintf(
		"Preferences: %s\nProfile: %s\nLeft Option Key: %s\nRight Option Key: %s",
		i.Path,
		i.Profile,
		iterm2OptionKeySendsLabel(i.LeftOptionSends),
		iterm2OptionKeySendsLabel(i.RightOptionSends),
	)
}

func inspectIterm2OptionBackspaceFix(env []string) (iterm2FixInspection, error) {
	path, err := iterm2PreferencesPath(env)
	if err != nil {
		return iterm2FixInspection{}, err
	}
	target, err := selectIterm2ProfileTarget(path, envValue(env, "ITERM_PROFILE"))
	if err != nil {
		return iterm2FixInspection{}, err
	}
	mappingConfigured, err := iterm2OptionBackspaceMappingConfigured(path, target.Index)
	if err != nil {
		return iterm2FixInspection{}, err
	}
	leftOption, rightOption, optionKeysConfigured, err := iterm2OptionKeysConfigured(path, target.Index)
	if err != nil {
		return iterm2FixInspection{}, err
	}
	return iterm2FixInspection{
		Path:              path,
		Profile:           target.Name,
		LeftOptionSends:   leftOption,
		RightOptionSends:  rightOption,
		MappingConfigured: mappingConfigured,
		CustomPreferences: iterm2UsingCustomPreferences(path),
		Configured:        mappingConfigured && optionKeysConfigured,
	}, nil
}

func iterm2ConfiguredButCurrentSessionStaleMessage(inspection iterm2FixInspection) string {
	lines := []string{
		fmt.Sprintf("iTerm2 profile %q already sets Left/Right Option Key to Esc+ and maps Option+Backspace to Esc DEL, but this terminal session is still sending plain Backspace.", inspection.Profile),
	}
	if inspection.CustomPreferences {
		lines = append(lines,
			"iTerm2 is loading settings from a custom folder, and this running iTerm2 process has not reloaded the external plist.",
			"Quit iTerm2 completely, reopen it, then rerun `weft doctor keys --clear`.",
		)
	} else {
		lines = append(lines,
			"Open a new iTerm2 tab or window with that profile, then rerun `weft doctor keys --clear`.",
			fmt.Sprintf("If a new tab still reports plain Backspace, restart iTerm2 so it reloads %s.", inspection.Path),
		)
	}
	return strings.Join(lines, "\n")
}

func iterm2OptionKeySendsLabel(value string) string {
	switch strings.TrimSpace(value) {
	case iTerm2OptionEscValue:
		return "Esc+"
	case "1":
		return "Meta"
	case "0":
		return "Normal"
	case "":
		return "unset"
	default:
		return value
	}
}

func iTerm2FixTargetSummary(env []string) string {
	path, err := iterm2PreferencesPath(env)
	if err != nil {
		return ""
	}
	profile := envValue(env, "ITERM_PROFILE")
	if target, err := selectIterm2ProfileTarget(path, profile); err == nil {
		profile = target.Name
	} else if profile == "" {
		profile = "default profile"
	}
	return fmt.Sprintf("Preferences: %s\nProfile: %s", path, profile)
}

func iterm2FixError(step string, path string, profile string, err error) error {
	lines := []string{
		"could not update iTerm2 Option+Backspace mapping",
		"step: " + step,
	}
	if path != "" {
		lines = append(lines, "preferences: "+path)
	}
	if profile != "" {
		lines = append(lines, "profile: "+profile)
	}
	lines = append(lines,
		"error: "+err.Error(),
		"Manual fix: iTerm2 > Settings > Profiles > Keys. Set Left/Right Option Key to Esc+. If needed, add Option+Backspace under Key Mappings and set it to Send Hex Codes `0x1b 0x7f`.",
	)
	return errors.New(strings.Join(lines, "\n"))
}

func iterm2PreferencesPath(env []string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	defaultPath := filepath.Join(home, "Library", "Preferences", "com.googlecode.iterm2.plist")
	loadCustom, found, err := plistExtractRawOptional(defaultPath, "LoadPrefsFromCustomFolder")
	if err != nil || !found || !truthyPlistRawValue(loadCustom) {
		return defaultPath, nil
	}
	customFolder, found, err := plistExtractRawOptional(defaultPath, "PrefsCustomFolder")
	if err != nil || !found || strings.TrimSpace(customFolder) == "" {
		return defaultPath, nil
	}
	customFolder = expandHomePath(customFolder, home)
	customPath := filepath.Join(customFolder, "com.googlecode.iterm2.plist")
	if _, err := os.Stat(customPath); err == nil {
		return customPath, nil
	}
	return defaultPath, nil
}

func iterm2UsingCustomPreferences(preferencesPath string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	defaultPath := filepath.Join(home, "Library", "Preferences", "com.googlecode.iterm2.plist")
	loadCustom, found, err := plistExtractRawOptional(defaultPath, "LoadPrefsFromCustomFolder")
	if err != nil || !found || !truthyPlistRawValue(loadCustom) {
		return false
	}
	customFolder, found, err := plistExtractRawOptional(defaultPath, "PrefsCustomFolder")
	if err != nil || !found || strings.TrimSpace(customFolder) == "" {
		return false
	}
	customPath := filepath.Join(expandHomePath(customFolder, home), "com.googlecode.iterm2.plist")
	return filepath.Clean(preferencesPath) == filepath.Clean(customPath)
}

type iterm2ProfileTarget struct {
	Index int
	Name  string
}

func selectIterm2ProfileTarget(path string, profileName string) (iterm2ProfileTarget, error) {
	count, err := iterm2ProfileCount(path)
	if err != nil {
		return iterm2ProfileTarget{}, err
	}
	if strings.TrimSpace(profileName) != "" {
		if target, ok, err := findIterm2ProfileTargetByName(path, count, profileName); err != nil {
			return iterm2ProfileTarget{}, err
		} else if ok {
			return target, nil
		}
	}
	if guid, ok, err := plistExtractRawOptional(path, "Default Bookmark Guid"); err != nil {
		return iterm2ProfileTarget{}, err
	} else if ok && guid != "" {
		if target, ok, err := findIterm2ProfileTargetByGuid(path, count, guid); err != nil {
			return iterm2ProfileTarget{}, err
		} else if ok {
			return target, nil
		}
	}
	if count > 0 {
		return iterm2ProfileTarget{Index: 0, Name: iterm2ProfileDisplayName(path, 0)}, nil
	}
	return iterm2ProfileTarget{}, errors.New("iTerm2 preferences do not contain a writable profile")
}

func iterm2ProfileCount(path string) (int, error) {
	raw, err := plistExtractRaw(path, "New Bookmarks")
	if err != nil {
		if errors.Is(err, errPlistKeyMissing) {
			return 0, errors.New("iTerm2 preferences do not contain profiles")
		}
		return 0, err
	}
	count, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("read profile count from iTerm2 preferences: %w", err)
	}
	if count <= 0 {
		return 0, errors.New("iTerm2 preferences do not contain profiles")
	}
	return count, nil
}

func findIterm2ProfileTargetByName(path string, count int, name string) (iterm2ProfileTarget, bool, error) {
	for i := 0; i < count; i++ {
		currentName, found, err := plistExtractRawOptional(path, fmt.Sprintf("New Bookmarks.%d.Name", i))
		if err != nil {
			return iterm2ProfileTarget{}, false, err
		}
		if found && currentName == name {
			return iterm2ProfileTarget{Index: i, Name: currentName}, true, nil
		}
	}
	return iterm2ProfileTarget{}, false, nil
}

func findIterm2ProfileTargetByGuid(path string, count int, guid string) (iterm2ProfileTarget, bool, error) {
	for i := 0; i < count; i++ {
		currentGuid, found, err := plistExtractRawOptional(path, fmt.Sprintf("New Bookmarks.%d.Guid", i))
		if err != nil {
			return iterm2ProfileTarget{}, false, err
		}
		if found && currentGuid == guid {
			return iterm2ProfileTarget{Index: i, Name: iterm2ProfileDisplayName(path, i)}, true, nil
		}
	}
	return iterm2ProfileTarget{}, false, nil
}

func iterm2ProfileDisplayName(path string, index int) string {
	if name, found, err := plistExtractRawOptional(path, fmt.Sprintf("New Bookmarks.%d.Name", index)); err == nil && found && name != "" {
		return name
	}
	if guid, found, err := plistExtractRawOptional(path, fmt.Sprintf("New Bookmarks.%d.Guid", index)); err == nil && found && guid != "" {
		return guid
	}
	return "selected profile"
}

func iterm2OptionBackspaceMappingConfigured(path string, profileIndex int) (bool, error) {
	entryPath := iterm2OptionBackspaceEntryKeyPath(profileIndex)
	action, actionFound, err := plistExtractRawOptional(path, entryPath+".Action")
	if err != nil {
		return false, err
	}
	text, textFound, err := plistExtractRawOptional(path, entryPath+".Text")
	if err != nil {
		return false, err
	}
	return actionFound && textFound && action == "11" && text == iTerm2OptionBackspaceText, nil
}

func iterm2OptionBackspaceFixConfigured(path string, profileIndex int) (bool, error) {
	mappingConfigured, err := iterm2OptionBackspaceMappingConfigured(path, profileIndex)
	if err != nil {
		return false, err
	}
	_, _, optionKeysConfigured, err := iterm2OptionKeysConfigured(path, profileIndex)
	if err != nil {
		return false, err
	}
	return mappingConfigured && optionKeysConfigured, nil
}

func iterm2OptionKeysConfigured(path string, profileIndex int) (string, string, bool, error) {
	left, _, err := plistExtractRawOptional(path, iterm2ProfileKeyPath(profileIndex, "Option Key Sends"))
	if err != nil {
		return "", "", false, err
	}
	right, _, err := plistExtractRawOptional(path, iterm2ProfileKeyPath(profileIndex, "Right Option Key Sends"))
	if err != nil {
		return "", "", false, err
	}
	return left, right, left == iTerm2OptionEscValue && right == iTerm2OptionEscValue, nil
}

func updateIterm2OptionBackspaceMappingFile(path string, profileIndex int) error {
	keyboardMapPath := fmt.Sprintf("New Bookmarks.%d.Keyboard Map", profileIndex)
	if _, found, err := plistExtractRawOptional(path, keyboardMapPath); err != nil {
		return err
	} else if !found {
		if err := plutilEdit("create keyboard map", path, "-insert", keyboardMapPath, "-dictionary"); err != nil {
			return err
		}
	}
	if err := upsertPlistInteger(path, iterm2ProfileKeyPath(profileIndex, "Option Key Sends"), iTerm2OptionEscValue); err != nil {
		return err
	}
	if err := upsertPlistInteger(path, iterm2ProfileKeyPath(profileIndex, "Right Option Key Sends"), iTerm2OptionEscValue); err != nil {
		return err
	}
	return plutilEdit(
		"write Option+Backspace key mapping",
		path,
		"-replace",
		iterm2OptionBackspaceEntryKeyPath(profileIndex),
		"-json",
		fmt.Sprintf(`{"Action":11,"Text":%q,"Version":1,"Label":""}`, iTerm2OptionBackspaceText),
	)
}

func iterm2OptionBackspaceEntryKeyPath(profileIndex int) string {
	return fmt.Sprintf("New Bookmarks.%d.Keyboard Map.%s", profileIndex, iTerm2OptionBackspaceKey)
}

func iterm2ProfileKeyPath(profileIndex int, key string) string {
	return fmt.Sprintf("New Bookmarks.%d.%s", profileIndex, key)
}

func plistExtractRaw(path string, keyPath string) (string, error) {
	args := []string{"/usr/bin/plutil", "-extract", keyPath, "raw", path}
	out, err := exec.Command(args[0], args[1:]...).CombinedOutput()
	if err != nil {
		if isMissingPlistKeyOutput(out) {
			return "", fmt.Errorf("%w: %s", errPlistKeyMissing, keyPath)
		}
		return "", commandOutputError("extract plist key", args, err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

func plistExtractRawOptional(path string, keyPath string) (string, bool, error) {
	value, err := plistExtractRaw(path, keyPath)
	if errors.Is(err, errPlistKeyMissing) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

func plutilEdit(action string, path string, args ...string) error {
	fullArgs := append([]string{"/usr/bin/plutil"}, args...)
	fullArgs = append(fullArgs, path)
	out, err := exec.Command(fullArgs[0], fullArgs[1:]...).CombinedOutput()
	if err != nil {
		return commandOutputError(action, fullArgs, err, out)
	}
	return nil
}

func upsertPlistInteger(path string, keyPath string, value string) error {
	if _, found, err := plistExtractRawOptional(path, keyPath); err != nil {
		return err
	} else if found {
		return plutilEdit("set plist integer", path, "-replace", keyPath, "-integer", value)
	}
	return plutilEdit("set plist integer", path, "-insert", keyPath, "-integer", value)
}

func commandOutputError(action string, args []string, err error, output []byte) error {
	lines := []string{
		fmt.Sprintf("%s failed: %v", action, err),
		"command: " + shellCommandForDisplay(args),
	}
	if text := strings.TrimSpace(string(output)); text != "" {
		lines = append(lines, "output: "+text)
	}
	return errors.New(strings.Join(lines, "\n"))
}

func shellCommandForDisplay(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, fmt.Sprintf("%q", arg))
	}
	return strings.Join(quoted, " ")
}

func backupFile(path string) (string, error) {
	backupPath := fmt.Sprintf("%s.weft-backup-%s", path, time.Now().Format("20060102-150405"))
	in, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.OpenFile(backupPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	return backupPath, nil
}

func isMissingPlistKeyOutput(output []byte) bool {
	text := string(output)
	return strings.Contains(text, "No value at that key path") || strings.Contains(text, "Key path not found")
}

func truthyPlistRawValue(value string) bool {
	value = strings.TrimSpace(value)
	return value == "1" || strings.EqualFold(value, "true") || strings.EqualFold(value, "yes")
}

func expandHomePath(path string, home string) string {
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}
