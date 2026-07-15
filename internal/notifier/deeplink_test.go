//go:build darwin

package notifier

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testCLISessionID = "6794614b-6b31-44e5-a6ed-e228105e5e3b"
	testAppSessionID = "local_5ecc1b29-3217-47db-8705-705d605a8349"
)

// writeSessionRecord drops a desktop-app style session metadata file into dir.
func writeSessionRecord(t *testing.T, dir, sessionID, cliSessionID string, archived bool, lastActivity int64) {
	t.Helper()
	rec := map[string]any{
		"sessionId":      sessionID,
		"cliSessionId":   cliSessionID,
		"isArchived":     archived,
		"lastActivityAt": lastActivity,
	}
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sessionID+".json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func withSessionsDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	orig := desktopSessionsDir
	desktopSessionsDir = func() string { return root }
	t.Cleanup(func() { desktopSessionsDir = orig })
	return root
}

func desktopEnv(t *testing.T) {
	t.Helper()
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "claude-desktop")
	t.Setenv("__CFBundleIdentifier", "com.anthropic.claudefordesktop")
}

func TestResolveDesktopSessionID(t *testing.T) {
	t.Run("prefers original conversation over resume mirror", func(t *testing.T) {
		root := withSessionsDir(t)
		nested := filepath.Join(root, "org-uuid", "account-uuid")
		writeSessionRecord(t, nested, "local_"+testCLISessionID, testCLISessionID, false, 200) // mirror, newer
		writeSessionRecord(t, nested, testAppSessionID, testCLISessionID, false, 100)          // original, older

		if got := resolveDesktopSessionID(testCLISessionID); got != testAppSessionID {
			t.Errorf("resolveDesktopSessionID() = %q, want %q", got, testAppSessionID)
		}
	})

	t.Run("falls back to mirror when it is the only record", func(t *testing.T) {
		root := withSessionsDir(t)
		writeSessionRecord(t, root, "local_"+testCLISessionID, testCLISessionID, false, 100)

		if got := resolveDesktopSessionID(testCLISessionID); got != "local_"+testCLISessionID {
			t.Errorf("resolveDesktopSessionID() = %q, want mirror id", got)
		}
	})

	t.Run("skips archived records", func(t *testing.T) {
		root := withSessionsDir(t)
		writeSessionRecord(t, root, testAppSessionID, testCLISessionID, true, 100)

		if got := resolveDesktopSessionID(testCLISessionID); got != "" {
			t.Errorf("resolveDesktopSessionID() = %q, want empty for archived-only", got)
		}
	})

	t.Run("no records returns empty", func(t *testing.T) {
		withSessionsDir(t)
		if got := resolveDesktopSessionID(testCLISessionID); got != "" {
			t.Errorf("resolveDesktopSessionID() = %q, want empty", got)
		}
	})
}

func TestBuildDesktopDeepLinkArgs(t *testing.T) {
	t.Run("desktop session builds app activation click", func(t *testing.T) {
		desktopEnv(t)

		args := buildDesktopDeepLinkArgs("✅ Completed", "done", testCLISessionID, true)
		if args == nil {
			t.Fatal("expected args, got nil")
		}
		joined := strings.Join(args, " ")
		want := "open -b 'com.anthropic.claudefordesktop'"
		if !strings.Contains(joined, want) {
			t.Errorf("args missing activation execute command %q: %s", want, joined)
		}
	})

	t.Run("cli session returns nil", func(t *testing.T) {
		t.Setenv("CLAUDE_CODE_ENTRYPOINT", "cli")
		t.Setenv("__CFBundleIdentifier", "")

		if args := buildDesktopDeepLinkArgs("t", "m", testCLISessionID, true); args != nil {
			t.Errorf("expected nil for cli session, got %v", args)
		}
	})

	t.Run("clickToFocus disabled returns nil", func(t *testing.T) {
		desktopEnv(t)

		if args := buildDesktopDeepLinkArgs("t", "m", testCLISessionID, false); args != nil {
			t.Errorf("expected nil when clickToFocus disabled, got %v", args)
		}
	})

	t.Run("non-uuid session id returns nil", func(t *testing.T) {
		desktopEnv(t)
		for _, id := range []string{"", "unknown", "local-debug", "abc'; rm -rf /"} {
			if args := buildDesktopDeepLinkArgs("t", "m", id, true); args != nil {
				t.Errorf("expected nil for session id %q, got %v", id, args)
			}
		}
	})
}
