//go:build darwin

package notifier

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework ApplicationServices -framework AppKit
#include <stdlib.h>
#import <AppKit/AppKit.h>
#import <ApplicationServices/ApplicationServices.h>

// axSessionFindPID returns the pid of the first running app with bundleID, or -1.
static int axSessionFindPID(const char *bundleID) {
	@autoreleasepool {
		NSString *bid = [NSString stringWithUTF8String:bundleID];
		NSArray *apps = [NSRunningApplication runningApplicationsWithBundleIdentifier:bid];
		if (!apps || apps.count == 0) return -1;
		return (int)((NSRunningApplication *)apps[0]).processIdentifier;
	}
}

// axSessionActivate brings the app with pid to the foreground.
static void axSessionActivate(int pid) {
	@autoreleasepool {
		NSRunningApplication *app = [NSRunningApplication runningApplicationWithProcessIdentifier:(pid_t)pid];
		if (!app) return;
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
		[app activateWithOptions:NSApplicationActivateIgnoringOtherApps];
#pragma clang diagnostic pop
	}
}

// promptForAXTrust triggers the system Accessibility permission prompt so the
// responsible app (ClaudeNotifier when run from a notification click) appears
// in System Settings > Privacy & Security > Accessibility.
static int promptForAXTrust(void) {
	@autoreleasepool {
		NSDictionary *opts = @{(__bridge NSString *)kAXTrustedCheckOptionPrompt: @YES};
		return AXIsProcessTrustedWithOptions((__bridge CFDictionaryRef)opts) ? 1 : 0;
	}
}

// enableElectronAX asks the Electron app to build its accessibility tree.
// Electron exposes web content to AX clients only after this (or after any
// assistive client connects).
static void enableElectronAX(int pid) {
	AXUIElementRef appEl = AXUIElementCreateApplication((pid_t)pid);
	if (!appEl) return;
	AXUIElementSetAttributeValue(appEl, CFSTR("AXManualAccessibility"), kCFBooleanTrue);
	CFRelease(appEl);
}

// titleMatchesSession reports whether an element title identifies the target
// conversation. Sidebar buttons carry the conversation title, optionally
// prefixed with a status ("Running <title>"), so match exact or suffix with a
// word boundary. Context-menu buttons ("More options for <title>") share the
// suffix and are excluded by role (they are AXPopUpButton, not AXButton).
static BOOL titleMatchesSession(NSString *title, NSString *target) {
	if ([title isEqualToString:target]) return YES;
	if ([title hasSuffix:[@" " stringByAppendingString:target]]) return YES;
	return NO;
}

// copyButtonName returns the accessible name of a button element: AXTitle when
// present, otherwise AXDescription (icon-only buttons like the Home/Code area
// switchers expose their name there). Caller releases.
static NSString *copyButtonName(AXUIElementRef el) {
	CFTypeRef ref = NULL;
	if (AXUIElementCopyAttributeValue(el, CFSTR("AXTitle"), &ref) == kAXErrorSuccess && ref) {
		if (CFGetTypeID(ref) == CFStringGetTypeID() && CFStringGetLength((CFStringRef)ref) > 0) {
			return (__bridge_transfer NSString *)ref;
		}
		CFRelease(ref);
	}
	ref = NULL;
	if (AXUIElementCopyAttributeValue(el, CFSTR("AXDescription"), &ref) == kAXErrorSuccess && ref) {
		if (CFGetTypeID(ref) == CFStringGetTypeID()) {
			return (__bridge_transfer NSString *)ref;
		}
		CFRelease(ref);
	}
	return nil;
}

// findAndPressSessionButton walks el looking for an AXButton whose title
// matches target and presses it. Returns 1 when pressed, 0 when not found.
static int findAndPressSessionButton(AXUIElementRef el, NSString *target, int depth, int *budget) {
	if (depth > 40 || *budget <= 0) return 0;
	(*budget)--;

	CFTypeRef roleRef = NULL;
	NSString *role = nil;
	if (AXUIElementCopyAttributeValue(el, CFSTR("AXRole"), &roleRef) == kAXErrorSuccess && roleRef) {
		if (CFGetTypeID(roleRef) == CFStringGetTypeID()) role = (__bridge NSString *)roleRef;
	}

	if (role && [role isEqualToString:@"AXButton"]) {
		NSString *name = copyButtonName(el);
		if (name && titleMatchesSession(name, target)) {
			if (roleRef) CFRelease(roleRef);
			AXUIElementPerformAction(el, CFSTR("AXScrollToVisible"));
			AXError pressErr = AXUIElementPerformAction(el, CFSTR("AXPress"));
			return pressErr == kAXErrorSuccess ? 1 : 0;
		}
	}
	if (roleRef) CFRelease(roleRef);

	CFTypeRef childrenRef = NULL;
	if (AXUIElementCopyAttributeValue(el, CFSTR("AXChildren"), &childrenRef) != kAXErrorSuccess || !childrenRef) {
		return 0;
	}
	int pressed = 0;
	CFArrayRef children = (CFArrayRef)childrenRef;
	CFIndex count = CFArrayGetCount(children);
	for (CFIndex i = 0; i < count && !pressed; i++) {
		AXUIElementRef child = (AXUIElementRef)CFArrayGetValueAtIndex(children, i);
		pressed = findAndPressSessionButton(child, target, depth + 1, budget);
	}
	CFRelease(childrenRef);
	return pressed;
}

// findAndPressExactButton walks el for an AXButton titled exactly target and
// presses it. Used for fixed navigation buttons like "Code" / "Home".
static int findAndPressExactButton(AXUIElementRef el, NSString *target, int depth, int *budget) {
	if (depth > 40 || *budget <= 0) return 0;
	(*budget)--;

	CFTypeRef roleRef = NULL;
	BOOL isButton = NO;
	if (AXUIElementCopyAttributeValue(el, CFSTR("AXRole"), &roleRef) == kAXErrorSuccess && roleRef) {
		isButton = CFGetTypeID(roleRef) == CFStringGetTypeID() &&
			[(__bridge NSString *)roleRef isEqualToString:@"AXButton"];
		CFRelease(roleRef);
	}
	if (isButton) {
		NSString *name = copyButtonName(el);
		if (name && [name isEqualToString:target]) {
			return AXUIElementPerformAction(el, CFSTR("AXPress")) == kAXErrorSuccess ? 1 : 0;
		}
	}

	CFTypeRef childrenRef = NULL;
	if (AXUIElementCopyAttributeValue(el, CFSTR("AXChildren"), &childrenRef) != kAXErrorSuccess || !childrenRef) {
		return 0;
	}
	int pressed = 0;
	CFArrayRef children = (CFArrayRef)childrenRef;
	CFIndex count = CFArrayGetCount(children);
	for (CFIndex i = 0; i < count && !pressed; i++) {
		pressed = findAndPressExactButton((AXUIElementRef)CFArrayGetValueAtIndex(children, i), target, depth + 1, budget);
	}
	CFRelease(childrenRef);
	return pressed;
}

// pressExactButtonInApp presses the first AXButton titled exactly targetTitle.
// Returns 1 pressed, 0 not found, -1 not trusted.
static int pressExactButtonInApp(int pid, const char *targetTitle) {
	@autoreleasepool {
		if (!AXIsProcessTrusted()) return -1;
		AXUIElementRef appEl = AXUIElementCreateApplication((pid_t)pid);
		if (!appEl) return 0;
		CFTypeRef windowsRef = NULL;
		if (AXUIElementCopyAttributeValue(appEl, CFSTR("AXWindows"), &windowsRef) != kAXErrorSuccess || !windowsRef) {
			CFRelease(appEl);
			return 0;
		}
		NSString *target = [NSString stringWithUTF8String:targetTitle];
		int pressed = 0;
		int budget = 200000;
		CFArrayRef windows = (CFArrayRef)windowsRef;
		CFIndex count = CFArrayGetCount(windows);
		for (CFIndex i = 0; i < count && !pressed; i++) {
			pressed = findAndPressExactButton((AXUIElementRef)CFArrayGetValueAtIndex(windows, i), target, 0, &budget);
		}
		CFRelease(windowsRef);
		CFRelease(appEl);
		return pressed;
	}
}

// pressSessionButtonInApp searches all windows of pid for the conversation
// button and presses it. Returns 1 pressed, 0 not found, -1 not trusted.
static int pressSessionButtonInApp(int pid, const char *targetTitle) {
	@autoreleasepool {
		if (!AXIsProcessTrusted()) return -1;

		AXUIElementRef appEl = AXUIElementCreateApplication((pid_t)pid);
		if (!appEl) return 0;

		CFTypeRef windowsRef = NULL;
		if (AXUIElementCopyAttributeValue(appEl, CFSTR("AXWindows"), &windowsRef) != kAXErrorSuccess || !windowsRef) {
			CFRelease(appEl);
			return 0;
		}

		NSString *target = [NSString stringWithUTF8String:targetTitle];
		int pressed = 0;
		int budget = 200000;
		CFArrayRef windows = (CFArrayRef)windowsRef;
		CFIndex count = CFArrayGetCount(windows);
		for (CFIndex i = 0; i < count && !pressed; i++) {
			AXUIElementRef win = (AXUIElementRef)CFArrayGetValueAtIndex(windows, i);
			pressed = findAndPressSessionButton(win, target, 0, &budget);
		}

		CFRelease(windowsRef);
		CFRelease(appEl);
		return pressed;
	}
}
*/
import "C"

import (
	"fmt"
	"time"
	"unsafe"

	"github.com/777genius/claude-notifications/internal/logging"
	"github.com/777genius/claude-notifications/internal/platform"
)

// FocusDesktopSessionByCLIID brings the Claude desktop app to the front and
// selects the conversation belonging to cliSessionID by pressing its sidebar
// item through the Accessibility API. The app is activated regardless of the
// outcome, so on any failure the behavior degrades to plain app focus.
func FocusDesktopSessionByCLIID(cliSessionID string) error {
	_, title := resolveDesktopSession(cliSessionID)

	cBundleID := C.CString(platform.DesktopAppBundleID)
	defer C.free(unsafe.Pointer(cBundleID))
	pid := int(C.axSessionFindPID(cBundleID))
	if pid < 0 {
		return fmt.Errorf("Claude desktop app is not running")
	}

	// Always bring the app forward first — matches previous click behavior
	// and gives Electron time to build its AX tree while animating.
	C.axSessionActivate(C.int(pid))

	if title == "" {
		return fmt.Errorf("no conversation title found for session %s", cliSessionID)
	}

	if C.promptForAXTrust() == 0 {
		return fmt.Errorf("accessibility permission not granted (grant it to Claude Notifier in System Settings > Privacy & Security > Accessibility)")
	}

	C.enableElectronAX(C.int(pid))

	cTitle := C.CString(title)
	defer C.free(unsafe.Pointer(cTitle))

	// The Electron AX tree builds asynchronously after enableElectronAX; the
	// sidebar also re-renders on app activation. Retry briefly. Claude Code
	// conversations are only listed in the sidebar while the "Code" area is
	// active, so after a couple of misses (user is in Home) switch areas and
	// keep looking.
	deadline := time.Now().Add(8 * time.Second)
	attempts := 0
	switchedArea := false
	for {
		switch C.pressSessionButtonInApp(C.int(pid), cTitle) {
		case 1:
			logging.Debug("Pressed sidebar item for conversation %q", title)
			return nil
		case -1:
			return fmt.Errorf("accessibility permission not granted")
		}
		attempts++
		if attempts >= 2 && !switchedArea {
			switchedArea = true
			cCode := C.CString("Code")
			if C.pressExactButtonInApp(C.int(pid), cCode) == 1 {
				logging.Debug("Conversation not in sidebar; switched app to Code area")
			}
			C.free(unsafe.Pointer(cCode))
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("conversation %q not found in app UI (app left focused)", title)
		}
		time.Sleep(400 * time.Millisecond)
	}
}
