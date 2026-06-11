// Package notify pops a desktop notification, best effort, no
// dependencies. It shells out to whatever each OS already ships and
// quietly does nothing if that tool isn't there.
package notify

import (
	"os/exec"
	"runtime"
	"strings"
)

func Send(title, msg string) {
	switch runtime.GOOS {
	case "darwin":
		script := "display notification " + osaStr(msg) + " with title " + osaStr(title)
		exec.Command("osascript", "-e", script).Run()
	case "linux":
		exec.Command("notify-send", title, msg).Run()
	case "windows":
		exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", winToast(title, msg)).Run()
	}
}

// osaStr wraps text as an AppleScript double-quoted string.
func osaStr(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// psStr wraps text as a PowerShell single-quoted string.
func psStr(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// winToast builds a one-line WinRT toast script. Works on Windows 10
// and 11 without any extra module installed.
func winToast(title, msg string) string {
	return strings.Join([]string{
		"$ErrorActionPreference='SilentlyContinue'",
		"$null=[Windows.UI.Notifications.ToastNotificationManager,Windows.UI.Notifications,ContentType=WindowsRuntime]",
		"$t=[Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02)",
		"$x=$t.GetElementsByTagName('text')",
		"$null=$x.Item(0).AppendChild($t.CreateTextNode(" + psStr(title) + "))",
		"$null=$x.Item(1).AppendChild($t.CreateTextNode(" + psStr(msg) + "))",
		"$n=[Windows.UI.Notifications.ToastNotification]::new($t)",
		"[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('mtop').Show($n)",
	}, ";")
}
