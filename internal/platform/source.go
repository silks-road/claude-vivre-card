package platform

import "os"

// SessionSource identifies which Claude surface invoked the hook.
type SessionSource string

const (
	SourceCLI     SessionSource = "cli"
	SourceDesktop SessionSource = "desktop" // Claude desktop app / Cowork sessions
	SourceSDK     SessionSource = "sdk"
	SourceUnknown SessionSource = "unknown"
)

// DesktopAppBundleID is the macOS bundle identifier of the Claude desktop app.
const DesktopAppBundleID = "com.anthropic.claudefordesktop"

// GetSessionSource classifies the current session from environment variables
// inherited from the Claude process that spawned the hook.
//
// CLAUDE_CODE_ENTRYPOINT is the primary signal ("claude-desktop" for Cowork,
// "cli" for the terminal CLI, "sdk-*" for Agent SDK embeddings). The
// __CFBundleIdentifier fallback covers desktop builds that predate the
// entrypoint variable.
func GetSessionSource() SessionSource {
	switch entrypoint := os.Getenv("CLAUDE_CODE_ENTRYPOINT"); {
	case entrypoint == "claude-desktop":
		return SourceDesktop
	case entrypoint == "cli":
		return SourceCLI
	case len(entrypoint) >= 3 && entrypoint[:3] == "sdk":
		return SourceSDK
	}

	if os.Getenv("__CFBundleIdentifier") == DesktopAppBundleID {
		return SourceDesktop
	}

	if os.Getenv("CLAUDECODE") == "1" {
		return SourceCLI
	}

	return SourceUnknown
}

// IsDesktopSession reports whether the hook was fired by the Claude desktop
// app (Cowork or the desktop Code tab) rather than a terminal CLI session.
func IsDesktopSession() bool {
	return GetSessionSource() == SourceDesktop
}
