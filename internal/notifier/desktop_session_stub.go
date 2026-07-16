//go:build !darwin

package notifier

import "fmt"

// resolveDesktopSession maps a CLI session id to the desktop app's own
// session record. The Claude desktop app integration is currently
// implemented for macOS only; other platforms fall back to standard focusing.
func resolveDesktopSession(cliSessionID string) (sessionID, title string) {
	return "", ""
}

// resolveDesktopSessionID returns just the desktop app's session id for a
// CLI session id. See resolveDesktopSession.
func resolveDesktopSessionID(cliSessionID string) string {
	return ""
}

// FocusDesktopSessionByCLIID is only implemented on macOS.
func FocusDesktopSessionByCLIID(cliSessionID string) error {
	return fmt.Errorf("focus-session not supported on this platform")
}
