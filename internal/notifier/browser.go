package notifier

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/777genius/claude-notifications/internal/analyzer"
	"github.com/777genius/claude-notifications/internal/logging"
	"github.com/777genius/claude-notifications/internal/platform"
)

// BrowserNotificationContent returns the uniform title and summarized body
// for a browser event without posting anything — used when the extension
// renders the notification itself (multi-browser correctness).
func (n *Notifier) BrowserNotificationContent(status analyzer.Status, title, message string) (string, string) {
	statusInfo, exists := n.cfg.GetStatusInfo(string(status))
	if !exists {
		return title, summarizeMessage(message, status)
	}
	notifTitle := statusInfo.Title
	if title != "" {
		notifTitle = fmt.Sprintf("%s - %s", statusInfo.Title, title)
	}
	return notifTitle, summarizeMessage(message, status)
}

// PlayStatusSound plays the configured sound for a status (detached).
func (n *Notifier) PlayStatusSound(status analyzer.Status) {
	if statusInfo, ok := n.cfg.GetStatusInfo(string(status)); ok {
		n.playSoundDetached(statusInfo.Sound)
	}
}

// SendCoworkTaskNotification posts a ✅ Done banner for a Cowork/Home task
// completion detected from the app log, through ClaudeNotifier, whose click
// navigates to that conversation in the desktop app (by wrapper id).
func (n *Notifier) SendCoworkTaskNotification(title, wrapperID string) error {
	executeCmd := ""
	if exe, err := os.Executable(); err == nil {
		if exe, err = filepath.EvalSymlinks(exe); err == nil {
			executeCmd = shellQuote(exe) + " focus-cowork " + shellQuote(wrapperID)
		}
	}
	return n.sendBrowserBanner(analyzer.StatusTaskComplete, title, "Task finished.", executeCmd)
}

// SendBrowserNotificationWithClick posts a browser-event banner through
// ClaudeNotifier (the identity users allow through macOS Focus) whose click
// hands the conversation id back to the listener (/focus) so the extension
// focuses the right tab in the right browser.
func (n *Notifier) SendBrowserNotificationWithClick(status analyzer.Status, title, message, conversationID string) error {
	executeCmd := ""
	if exe, err := os.Executable(); err == nil {
		if exe, err = filepath.EvalSymlinks(exe); err == nil {
			executeCmd = shellQuote(exe) + " request-browser-focus " + shellQuote(conversationID)
		}
	}
	return n.sendBrowserBanner(status, title, message, executeCmd)
}

// SendBrowserNotification posts a notification for a browser (claude.ai) event.
// Unlike SendDesktop (terminal/desktop-app sessions), the click opens the chat
// URL in the default browser. title is the conversation title, message the body
// (already summarized by the caller is fine; it is summarized again defensively),
// and chatURL the https://claude.ai/chat/<id> link.
func (n *Notifier) SendBrowserNotification(status analyzer.Status, title, message, chatURL string) error {
	if !n.cfg.IsDesktopEnabled() {
		logging.Debug("Desktop notifications disabled, skipping browser notification")
		return nil
	}

	statusInfo, exists := n.cfg.GetStatusInfo(string(status))
	if !exists {
		return fmt.Errorf("unknown status: %s", status)
	}

	notifTitle := statusInfo.Title
	if title != "" {
		notifTitle = fmt.Sprintf("%s - %s", statusInfo.Title, title)
	}
	body := summarizeMessage(message, status)

	if !platform.IsMacOS() {
		// Non-macOS: best-effort delivery without click-to-open.
		if err := n.sendWithBeeep(notifTitle, body, n.cfg.Notifications.Desktop.AppIcon, statusInfo.Sound); err != nil {
			return err
		}
		return nil
	}

	var executeCmd string
	if chatURL != "" {
		executeCmd = "open " + shellQuote(chatURL)
	}
	return n.sendBrowserBannerPrepared(status, notifTitle, body, executeCmd)
}

// sendBrowserBanner builds uniform content then posts via ClaudeNotifier.
func (n *Notifier) sendBrowserBanner(status analyzer.Status, title, message, executeCmd string) error {
	if !n.cfg.IsDesktopEnabled() {
		return nil
	}
	notifTitle, body := n.BrowserNotificationContent(status, title, message)
	return n.sendBrowserBannerPrepared(status, notifTitle, body, executeCmd)
}

// sendBrowserBannerPrepared posts a ready-made banner via ClaudeNotifier.
func (n *Notifier) sendBrowserBannerPrepared(status analyzer.Status, notifTitle, body, executeCmd string) error {
	statusInfo, exists := n.cfg.GetStatusInfo(string(status))
	if !exists {
		return fmt.Errorf("unknown status: %s", status)
	}
	notifierPath, err := GetTerminalNotifierPath()
	if err != nil {
		return fmt.Errorf("terminal-notifier not found: %w", err)
	}
	args := []string{"-title", notifTitle, "-message", body}
	if executeCmd != "" {
		args = append(args, "-execute", executeCmd)
	}
	args = append(args, "-group", fmt.Sprintf("claude-browser-%d", time.Now().UnixNano()))
	if isTimeSensitiveStatus(status) {
		args = append(args, "-timeSensitive")
	}
	args = append(args, "-nosound")

	if appPath, ok := claudeNotifierAppPath(notifierPath); ok {
		if err := runClaudeNotifierApp(appPath, args); err != nil {
			return err
		}
	} else if output, err := buildNotifierCommand(notifierPath, args).CombinedOutput(); err != nil {
		return fmt.Errorf("terminal-notifier error: %w, output: %s", err, string(output))
	}

	logging.Debug("Browser notification sent: title=%s", notifTitle)
	n.playSoundDetached(statusInfo.Sound)
	return nil
}
