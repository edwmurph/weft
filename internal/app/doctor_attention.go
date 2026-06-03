package app

import (
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
)

const iTerm2NotificationAlertsKey = "BM Growl"

type iterm2AttentionInspection struct {
	Path              string
	Profile           string
	Enabled           bool
	CustomPreferences bool
}

func doctorAttention(input io.Reader, output io.Writer, env []string) error {
	detected := detectDoctorTerminal(env)
	fmt.Fprintln(output, "Weft attention doctor")
	if detected.Name != "" {
		fmt.Fprintf(output, "Detected terminal: %s\n", detected.Name)
	}
	if runtime.GOOS != "darwin" {
		fmt.Fprintln(output, "Automatic iTerm2 notification checks are only supported on macOS.")
		return nil
	}
	if detected.Kind != "iterm2" {
		fmt.Fprintln(output, "Automatic notification configuration currently supports iTerm2.")
		fmt.Fprintln(output, "Run this command from an iTerm2 tab to target the current profile.")
		return nil
	}
	inspection, err := inspectIterm2Attention(env)
	if err != nil {
		return iterm2AttentionError("inspect notification settings", "", envValue(env, "ITERM_PROFILE"), err)
	}
	fmt.Fprintln(output, inspection.Summary())
	if inspection.Enabled {
		fmt.Fprintln(output, "OK: iTerm2 Notification Center alerts are enabled for this profile.")
		emitIterm2DoctorTestNotification(output)
		fmt.Fprintln(output, iterm2AttentionManualChecks())
		return nil
	}
	fmt.Fprintln(output, "Issue: iTerm2 Notification Center alerts are disabled for this profile.")
	fmt.Fprintln(output, "Weft can enable them for the current/default profile after writing a backup.")
	if !confirmWithIO(input, output, "Apply this iTerm2 notification fix now? [y/N] ") {
		fmt.Fprintln(output, "No terminal settings changed.")
		fmt.Fprintln(output, iterm2AttentionManualChecks())
		return nil
	}
	message, err := applyIterm2AttentionFix(env)
	if err != nil {
		return err
	}
	fmt.Fprintln(output, message)
	fmt.Fprintln(output, iterm2AttentionManualChecks())
	return nil
}

func inspectIterm2Attention(env []string) (iterm2AttentionInspection, error) {
	path, err := iterm2PreferencesPath(env)
	if err != nil {
		return iterm2AttentionInspection{}, err
	}
	target, err := selectIterm2ProfileTarget(path, envValue(env, "ITERM_PROFILE"))
	if err != nil {
		return iterm2AttentionInspection{}, err
	}
	enabled, err := iterm2NotificationAlertsEnabled(path, target.Index)
	if err != nil {
		return iterm2AttentionInspection{}, err
	}
	return iterm2AttentionInspection{
		Path:              path,
		Profile:           target.Name,
		Enabled:           enabled,
		CustomPreferences: iterm2UsingCustomPreferences(path),
	}, nil
}

func (i iterm2AttentionInspection) Summary() string {
	status := "disabled"
	if i.Enabled {
		status = "enabled"
	}
	return fmt.Sprintf(
		"Preferences: %s\nProfile: %s\nNotification Center alerts: %s",
		i.Path,
		i.Profile,
		status,
	)
}

func applyIterm2AttentionFix(env []string) (string, error) {
	path, err := iterm2PreferencesPath(env)
	if err != nil {
		return "", iterm2AttentionError("locate preferences", "", envValue(env, "ITERM_PROFILE"), err)
	}
	target, err := selectIterm2ProfileTarget(path, envValue(env, "ITERM_PROFILE"))
	if err != nil {
		return "", iterm2AttentionError("select profile", path, envValue(env, "ITERM_PROFILE"), err)
	}
	enabled, err := iterm2NotificationAlertsEnabled(path, target.Index)
	if err != nil {
		return "", iterm2AttentionError("read notification settings", path, target.Name, err)
	}
	if enabled {
		return fmt.Sprintf("iTerm2 profile %q already enables Notification Center alerts.", target.Name), nil
	}
	backupPath, err := backupFile(path)
	if err != nil {
		return "", iterm2AttentionError("write backup", path, target.Name, err)
	}
	if err := updateIterm2NotificationAlertsFile(path, target.Index); err != nil {
		return "", iterm2AttentionError("write updated preferences", path, target.Name, err)
	}
	lines := []string{
		fmt.Sprintf("Updated iTerm2 profile %q.", target.Name),
		"Backup: " + backupPath,
	}
	if iterm2UsingCustomPreferences(path) {
		lines = append(lines, "iTerm2 is loading settings from a custom folder; quit and reopen iTerm2, then rerun `weft doctor attention`.")
	} else {
		lines = append(lines, "Open a new iTerm2 tab or window, then rerun `weft doctor attention`.")
	}
	return strings.Join(lines, "\n"), nil
}

func iterm2NotificationAlertsEnabled(path string, profileIndex int) (bool, error) {
	value, found, err := plistExtractRawOptional(path, iterm2ProfileKeyPath(profileIndex, iTerm2NotificationAlertsKey))
	if err != nil {
		return false, err
	}
	return found && truthyPlistRawValue(value), nil
}

func updateIterm2NotificationAlertsFile(path string, profileIndex int) error {
	return upsertPlistBool(path, iterm2ProfileKeyPath(profileIndex, iTerm2NotificationAlertsKey), true)
}

func iterm2AttentionManualChecks() string {
	return strings.Join([]string{
		"Notification test: printf '\\033]9;Weft test notification\\a'",
		"If the test notification did not appear, check iTerm2 > Settings > Profiles > Terminal > Notifications > Filter Alerts and allow escape sequence-generated alerts.",
		"Also check macOS System Settings > Notifications > iTerm2 and allow notifications.",
	}, "\n")
}

func emitIterm2DoctorTestNotification(output io.Writer) {
	fmt.Fprint(output, "\x1b]9;Weft test notification\a")
	fmt.Fprintln(output, "Sent iTerm2 test notification.")
}

func iterm2AttentionError(step string, path string, profile string, err error) error {
	lines := []string{
		"could not update iTerm2 notification settings",
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
		"Manual fix: iTerm2 > Settings > Profiles > Terminal > Notifications. Enable Notification Center alerts, then use Filter Alerts to allow escape sequence-generated alerts.",
	)
	return errors.New(strings.Join(lines, "\n"))
}
