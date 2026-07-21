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

// desktopAppIsFrontmost is only meaningful on macOS.
func desktopAppIsFrontmost() bool { return false }

// isDesktopSessionViewed is only meaningful on macOS.
func isDesktopSessionViewed(cliSessionID string) bool { return false }

// ResolveDesktopSessionByWrapper is only implemented on macOS.
func ResolveDesktopSessionByWrapper(wrapperID string) (cliSessionID, title string) {
	return "", ""
}

// CurrentFocusedDesktopSession is only implemented on macOS.
func CurrentFocusedDesktopSession() string { return "" }

// FocusDesktopSessionByWrapper is only implemented on macOS.
func FocusDesktopSessionByWrapper(wrapperID string) error {
	return fmt.Errorf("focus-cowork not supported on this platform")
}

// RespondDesktopApproval is only implemented on macOS.
func RespondDesktopApproval(cliSessionID, scope string) error {
	return fmt.Errorf("respond-approval not supported on this platform")
}

// DesktopAppLogSize is only meaningful on macOS.
func DesktopAppLogSize() int64 { return 0 }

// WatchDesktopApprovals is only implemented on macOS.
func (n *Notifier) WatchDesktopApprovals(cliSessionID, cwd string, logOffset int64) error {
	return fmt.Errorf("approval watching not supported on this platform")
}
