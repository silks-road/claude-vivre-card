//go:build darwin

package notifier

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/777genius/claude-notifications/internal/logging"
)

// desktopSessionRecord is the subset of the Claude desktop app's per-session
// metadata file we need to map a hook's session id to the app's own id.
type desktopSessionRecord struct {
	SessionID    string `json:"sessionId"`
	CLISessionID string `json:"cliSessionId"`
	IsArchived   bool   `json:"isArchived"`
	LastActivity int64  `json:"lastActivityAt"`
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

// resolveDesktopSessionID maps the CLI-level session id received by hooks to
// the desktop app's own session id (e.g. "local_<other-uuid>"), which is what
// the app's /cowork/<id> route expects.
//
// A conversation created in the app has sessionId != "local_"+cliSessionId;
// a session imported via the claude://resume deep link is named exactly
// "local_"+cliSessionId (a mirror). Prefer the former so clicks land on the
// original conversation, and fall back to the mirror only if it is all we have.
func resolveDesktopSessionID(cliSessionID string) string {
	root := desktopSessionsDir()
	if root == "" || cliSessionID == "" {
		return ""
	}

	mirrorID := "local_" + cliSessionID
	best := ""
	var bestActivity int64 = -1

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
		bestIsMirror := best == mirrorID
		switch {
		case best == "":
			best, bestActivity = rec.SessionID, rec.LastActivity
		case bestIsMirror && !isMirror:
			best, bestActivity = rec.SessionID, rec.LastActivity
		case bestIsMirror == isMirror && rec.LastActivity > bestActivity:
			best, bestActivity = rec.SessionID, rec.LastActivity
		}
		return nil
	})

	if best == "" {
		logging.Debug("No desktop session record found for cli session %s", cliSessionID)
	}
	return best
}
