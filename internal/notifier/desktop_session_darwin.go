//go:build darwin

package notifier

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/777genius/claude-notifications/internal/logging"
	"github.com/777genius/claude-notifications/internal/platform"
)

// desktopSessionRecord is the subset of the Claude desktop app's per-session
// metadata file we need to map a hook's session id to the app's own id.
type desktopSessionRecord struct {
	SessionID    string `json:"sessionId"`
	CLISessionID string `json:"cliSessionId"`
	Title        string `json:"title"`
	IsArchived   bool   `json:"isArchived"`
	LastActivity int64  `json:"lastActivityAt"`
	LastFocused  int64  `json:"lastFocusedAt"`
}

// desktopAppIsFrontmost reports whether the Claude desktop app is the
// frontmost application.
func desktopAppIsFrontmost() bool {
	front, ok := frontmostBundleID()
	return ok && front == platform.DesktopAppBundleID
}

// setFocusedSessionRe extracts the session id from the desktop app's
// "LocalSessions.setFocusedSession: sessionId=<id|null>" log lines — the app
// emits one on every conversation switch, making the LAST occurrence the
// ground truth for what is on screen.
var setFocusedSessionRe = regexp.MustCompile(`LocalSessions\.setFocusedSession: sessionId=(\S+)`)

// currentFocusedDesktopSession returns the app-session id currently shown in
// the desktop app, from the tail of the app log. Returns "" when unknown
// (no line in the tail, log missing, or sessionId=null).
func currentFocusedDesktopSession() string {
	logPath := desktopAppLogPath()
	if logPath == "" {
		return ""
	}
	info, err := os.Stat(logPath)
	if err != nil {
		return ""
	}
	const tailBytes = 256 * 1024
	offset := info.Size() - tailBytes
	if offset < 0 {
		offset = 0
	}
	data, err := readFileFrom(logPath, offset)
	if err != nil {
		return ""
	}
	matches := setFocusedSessionRe.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return ""
	}
	last := string(matches[len(matches)-1][1])
	if last == "null" {
		return ""
	}
	return last
}

// isDesktopSessionViewed reports whether the conversation for cliSessionID is
// the one currently shown in the desktop app. Uses the app's own
// setFocusedSession log lines — the previous lastFocusedAt-file heuristic was
// written on LEAVING a conversation, so the chat the user just left looked
// "viewed" and its notifications were wrongly suppressed. Fails open to
// "not viewed" (notify) on any uncertainty.
func isDesktopSessionViewed(cliSessionID string) bool {
	focused := currentFocusedDesktopSession()
	if focused == "" || cliSessionID == "" {
		return false
	}
	wrapperID, _ := resolveDesktopSession(cliSessionID)
	return wrapperID != "" && wrapperID == focused
}

// desktopSessionsDir returns the root the desktop app stores session metadata
// under (~/Library/Application Support/Claude/claude-code-sessions).
// Overridable for tests.
var desktopSessionsDir = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "Application Support", "Claude", "claude-code-sessions")
}

// ResolveDesktopSessionByWrapper is the inverse of resolveDesktopSession: given
// the desktop app's own session id (the "local_..." wrapper id that appears in
// the app log), it returns the CLI session id and the conversation title.
// Used by the Cowork-log watcher to identify a completed session and dedupe it
// against hook-driven notifications. Returns "","" when no record matches.
func ResolveDesktopSessionByWrapper(wrapperID string) (cliSessionID, title string) {
	root := desktopSessionsDir()
	if root == "" || wrapperID == "" {
		return "", ""
	}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		var rec desktopSessionRecord
		if json.Unmarshal(data, &rec) != nil {
			return nil
		}
		if rec.SessionID == wrapperID {
			cliSessionID, title = rec.CLISessionID, rec.Title
			return filepath.SkipAll
		}
		return nil
	})
	return cliSessionID, title
}

// CurrentFocusedDesktopSession returns the wrapper id of the conversation
// currently shown in the desktop app (exported for the Cowork-log watcher).
func CurrentFocusedDesktopSession() string { return currentFocusedDesktopSession() }

// resolveDesktopSession maps the CLI-level session id received by hooks to
// the desktop app's own session record: its session id (e.g.
// "local_<other-uuid>") and the conversation title shown in the app sidebar.
//
// A conversation created in the app has sessionId != "local_"+cliSessionId;
// a session imported via the claude://resume deep link is named exactly
// "local_"+cliSessionId (a mirror). Prefer the former so callers target the
// original conversation, and fall back to the mirror only if it is all we have.
func resolveDesktopSession(cliSessionID string) (sessionID, title string) {
	root := desktopSessionsDir()
	if root == "" || cliSessionID == "" {
		return "", ""
	}

	mirrorID := "local_" + cliSessionID
	var best desktopSessionRecord
	bestActivity := int64(-1)

	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		var rec desktopSessionRecord
		if json.Unmarshal(data, &rec) != nil {
			return nil
		}
		if rec.CLISessionID != cliSessionID || rec.SessionID == "" || rec.IsArchived {
			return nil
		}
		// Prefer any non-mirror record; among candidates pick the most recent.
		isMirror := rec.SessionID == mirrorID
		bestIsMirror := best.SessionID == mirrorID
		switch {
		case best.SessionID == "":
			best, bestActivity = rec, rec.LastActivity
		case bestIsMirror && !isMirror:
			best, bestActivity = rec, rec.LastActivity
		case bestIsMirror == isMirror && rec.LastActivity > bestActivity:
			best, bestActivity = rec, rec.LastActivity
		}
		return nil
	})

	if best.SessionID == "" {
		logging.Debug("No desktop session record found for cli session %s", cliSessionID)
	}
	return best.SessionID, best.Title
}

// resolveDesktopSessionID returns just the desktop app's session id for a
// CLI session id. See resolveDesktopSession.
func resolveDesktopSessionID(cliSessionID string) string {
	id, _ := resolveDesktopSession(cliSessionID)
	return id
}
