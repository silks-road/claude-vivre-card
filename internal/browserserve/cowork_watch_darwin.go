//go:build darwin

package browserserve

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/777genius/claude-notifications/internal/logging"
	"github.com/777genius/claude-notifications/internal/notifier"
)

// The Claude desktop app runs some tasks (Cowork "Home" tasks) in the cloud,
// producing no hooks, no transcript, and no session record this plugin's hook
// path can see. But the app DOES log their turn completions to its main log:
//
//	[Result] Turn succeeded for session local_<wrapperID>
//
// This watcher tails that log and re-emits a notification for completions that
// our hooks did NOT already handle — i.e. Home tasks — so they get the same
// rich banner (title, sound, click-to-navigate) as every other surface, with
// no duplicate for local sessions (which the hooks own). The native "Claude"
// app notifications are set to no on-screen surface by the user, so this is
// the sole visible notification.

var coworkTurnDoneRe = regexp.MustCompile(`\[Result\] Turn succeeded for session (local_[0-9a-fA-F-]+)`)

// coworkTitleRe extracts a conversation title from the app log line
// "Updated session local_X: { title: 'Some Title', ... }" — the title source
// for Home tasks, which have no session-record file on disk.
var coworkTitleRe = regexp.MustCompile(`Updated session (local_[0-9a-fA-F-]+):.*?title: '([^']*)'`)

// titleFromLogData scans log bytes for the most recent title assigned to
// wrapperID. Returns "" if none found. Used when no session record exists.
func titleFromLogData(data []byte, wrapperID string) string {
	matches := coworkTitleRe.FindAllSubmatch(data, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		if string(matches[i][1]) == wrapperID {
			return string(matches[i][2])
		}
	}
	return ""
}

// coworkLogPath returns the desktop app's main log. Overridable for tests.
var coworkLogPath = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "Logs", "Claude", "main.log")
}

// sessionIsHookCovered reports whether wrapperID belongs to a local session
// our Stop hook handles — in which case the hook already sent a rich
// notification and this watcher must NOT duplicate it. The desktop app logs
// "[Stop hook] Query completed for session <wrapperID>" every time our hook
// runs, and ONLY for hook-covered sessions; cloud/Home tasks never produce it
// (verified: 255 lines for a local session, 0 for a Home task). A session is
// local-or-cloud for its whole life, so presence anywhere in the recent log
// tail is a reliable, permission-free, cross-process-safe discriminator —
// no state files, no shared-tmp assumptions.
func sessionIsHookCovered(logData []byte, wrapperID string) bool {
	return strings.Contains(string(logData), "[Stop hook] Query completed for session "+wrapperID)
}

// WatchCoworkLog tails the desktop app log and re-emits notifications for
// cloud/Home task completions the hook path can't see. Runs for the life of
// the listener process. Safe no-op if the log is unreadable.
func (s *Server) WatchCoworkLog() {
	logPath := coworkLogPath()
	if logPath == "" {
		return
	}

	// Start at end of file — only react to completions from here on.
	var offset int64
	if info, err := os.Stat(logPath); err == nil {
		offset = info.Size()
	}

	n := notifier.New(s.cfg)
	lastNotified := map[string]time.Time{} // wrapperID → when (self-dedupe)

	for {
		time.Sleep(2 * time.Second)

		info, err := os.Stat(logPath)
		if err != nil {
			continue
		}
		if info.Size() < offset {
			offset = 0 // log rotated
		}
		if info.Size() == offset {
			continue
		}

		f, err := os.Open(logPath)
		if err != nil {
			continue
		}
		if _, err := f.Seek(offset, 0); err != nil {
			f.Close()
			continue
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			m := coworkTurnDoneRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			s.handleCoworkCompletion(n, m[1], lastNotified)
		}
		offset, _ = f.Seek(0, 1)
		f.Close()
	}
}

func (s *Server) handleCoworkCompletion(n *notifier.Notifier, wrapperID string, lastNotified map[string]time.Time) {
	// Self-dedupe: the same completion line can be re-read across polls only if
	// offset math slips; also the app logs some lines twice. Guard 10s.
	if t, ok := lastNotified[wrapperID]; ok && time.Since(t) < 10*time.Second {
		return
	}

	// Dedup vs hooks: skip local sessions our Stop hook already notified.
	logData, _ := os.ReadFile(coworkLogPath())
	if sessionIsHookCovered(logData, wrapperID) {
		return
	}

	_, title := notifier.ResolveDesktopSessionByWrapper(wrapperID)
	// Home tasks have no session-record file; recover the title from the log.
	if title == "" {
		title = titleFromLogData(logData, wrapperID)
	}

	// Viewing suppression: don't ping the conversation on screen.
	if notifier.CurrentFocusedDesktopSession() == wrapperID {
		return
	}

	lastNotified[wrapperID] = time.Now()
	if title == "" {
		title = "Cowork task"
	}
	logging.Debug("cowork-watch: re-emitting for uncovered session %s (%q)", wrapperID, title)
	if err := n.SendCoworkTaskNotification(title, wrapperID); err != nil {
		logging.Warn("cowork-watch: notify failed: %v", err)
	}
	// Trim the map so it can't grow unbounded.
	if len(lastNotified) > 256 {
		cutoff := time.Now().Add(-5 * time.Minute)
		for k, t := range lastNotified {
			if t.Before(cutoff) {
				delete(lastNotified, k)
			}
		}
	}
}
