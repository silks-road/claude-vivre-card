package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/777genius/claude-notifications/internal/browserserve"
	"github.com/777genius/claude-notifications/internal/errorhandler"
	"github.com/777genius/claude-notifications/internal/notifier"
)

// runSetup is the one-command guided installer: it performs every step that
// can be automated and prints a precise checklist of the few macOS toggles
// only a human may click. Output is structured so the /setup slash command
// can walk the user through the remainder interactively.
//
// Idempotent: safe to re-run any time; already-done steps report as such.
func runSetup() {
	defer errorhandler.HandlePanic()

	if currentGOOS == "windows" {
		runSetupWindows()
		return
	}
	if currentGOOS != "darwin" {
		fmt.Println("Guided setup currently covers macOS and Windows. On Linux, follow the README's manual install.")
		return
	}

	fmt.Println("── Claude Notifications: guided setup ─────────────────────")

	// Step 1: notifier app present?
	notifierOK := notifier.IsTerminalNotifierAvailable()
	if notifierOK {
		fmt.Println("[done] Notifier app installed")
	} else {
		fmt.Println("[action-needed] Notifier app missing — run /claude-notifications-go:init first, then re-run setup")
	}

	// Step 2: trigger the macOS notification-permission prompt by sending a
	// hello notification. If permission was already granted this simply shows.
	if notifierOK {
		err := notifier.SendQuickNotification(
			"👋 Claude Notifications",
			"Setup is running - if you can see this, notifications work.",
			"")
		if err != nil {
			fmt.Println("[you] Notifications: macOS is blocking them. Open System Settings > Notifications > Claude Notifier -> Allow, style 'Alerts'. (A settings window may open now.)")
			_ = exec.Command("open", "x-apple.systempreferences:com.apple.preference.notifications").Start()
		} else {
			fmt.Println("[check] A hello notification was just sent - if it appeared as a banner, notifications are set. If it did NOT appear: System Settings > Notifications > Claude Notifier -> Allow, style 'Alerts'.")
		}
	}

	// Step 3: browser listener (token + LaunchAgent). Reuses existing logic;
	// prints the token for the extension popup.
	fmt.Println("── Browser notifications (claude.ai) ──────────────────────")
	runInstallBrowserListener()

	// Step 4: reveal the extension folder for chrome://extensions -> Load unpacked.
	pluginRoot := getPluginRoot()
	extDir := filepath.Join(pluginRoot, "extension")
	if _, err := os.Stat(extDir); err == nil {
		fmt.Printf("[you] Load the extension: chrome://extensions -> enable Developer mode -> 'Load unpacked' -> select this folder (opening in Finder now):\n      %s\n", extDir)
		_ = exec.Command("open", extDir).Start()
	} else {
		fmt.Println("[skip] Extension folder not found in plugin dir - browser notifications need a git clone of the repo")
	}

	// Step 5: Accessibility (click-to-conversation + approval buttons).
	fmt.Println("── Click-to-conversation & approval buttons ────────────────")
	notifierApp := filepath.Join(pluginRoot, "bin", "ClaudeNotifier.app")
	if _, err := os.Stat(notifierApp); err == nil {
		fmt.Printf("[you] Accessibility: System Settings > Privacy & Security > Accessibility -> add & enable ClaudeNotifier.app (drag it in from the Finder window that opens now).\n")
		_ = exec.Command("open", "-R", notifierApp).Start()
		_ = exec.Command("open", "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility").Start()
	}

	// Step 6: Focus-mode reminder (cannot be inspected programmatically).
	fmt.Println("[you] Using Focus modes? Add 'Claude' and 'Claude Notifier' to the allowed apps of the Focus you use.")

	fmt.Println("── Summary ─────────────────────────────────────────────────")
	fmt.Println("Automated steps are done. Human steps are the lines marked [you] above —")
	fmt.Println("the relevant Settings panes and Finder windows have been opened for you.")
	fmt.Printf("Extension token (paste into the extension's popup):\n  %s\n", browserserve.LoadToken())
}

// runSetupWindows is the Windows guided setup. Windows toasts need no
// permission grants, so there is less to do: verify delivery with a hello
// notification and state honestly which fork features are macOS-only today.
func runSetupWindows() {
	fmt.Println("── Claude Notifications: guided setup (Windows) ────────────")

	err := notifier.SendQuickNotification(
		"👋 Claude Notifications",
		"Setup is running - if you can see this, notifications work.",
		"")
	if err != nil {
		fmt.Println("[you] The hello notification failed to send. Check Windows Settings > System > Notifications: notifications must be ON, and 'claude-notifications' (or your terminal app) must be allowed.")
	} else {
		fmt.Println("[check] A hello notification was just sent. If it did NOT appear: Windows Settings > System > Notifications -> ensure notifications are ON and not silenced by Focus Assist / Do Not Disturb.")
	}

	fmt.Println("[done] Terminal notifications, sounds, uniform titles, question detection and webhooks are active.")
	fmt.Println("[note] macOS-only for now: desktop-app click-to-conversation, approval buttons, and the browser extension. They are on the roadmap for Windows.")
	fmt.Println("── Summary ─────────────────────────────────────────────────")
	fmt.Println("Windows setup needs no permission toggles. You're done - ask a Claude session to do something and enjoy the ping.")
}
