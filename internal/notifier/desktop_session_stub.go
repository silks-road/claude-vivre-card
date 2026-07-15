//go:build !darwin

package notifier

// resolveDesktopSessionID maps a CLI session id to the desktop app's own
// session id. The Claude desktop app deep-link integration is currently
// implemented for macOS only; other platforms fall back to standard focusing.
func resolveDesktopSessionID(cliSessionID string) string {
	return ""
}
