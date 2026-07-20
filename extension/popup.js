// popup.js — save the shared token and test the local listener.

const tokenInput = document.getElementById("token");
const statusEl = document.getElementById("status");

// Prefill existing token.
chrome.storage.local.get("token").then(({ token }) => {
  if (token) tokenInput.value = token;
});

document.getElementById("save").addEventListener("click", async () => {
  const token = tokenInput.value.trim();
  if (!token) {
    show("Enter a token first.", "err");
    return;
  }
  await chrome.storage.local.set({ token });
  show("Saved. Testing listener…", "");

  try {
    const resp = await fetch("http://127.0.0.1:52741/event", {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Auth-Token": token },
      body: JSON.stringify({
        conversationId: "test",
        title: "Extension test",
        lastMessage: "Browser notifications are connected.",
        url: "https://claude.ai",
        status: "task_complete",
      }),
    });
    if (resp.ok) {
      show("✓ Connected — you should see a notification.", "ok");
    } else if (resp.status === 401) {
      show("Token rejected (401). Re-copy the token from the installer.", "err");
    } else {
      show("Listener error: " + resp.status, "err");
    }
  } catch (e) {
    show("Can't reach the listener. Run: claude-notifications install-browser-listener", "err");
  }
});

function show(msg, cls) {
  statusEl.textContent = msg;
  statusEl.className = cls;
}


// Test banner: isolates banner rendering from the detection/listener pipeline.
document.getElementById("testbanner").addEventListener("click", () => {
  const diag = document.getElementById("diag");
  chrome.notifications.getPermissionLevel((level) => {
    let msg = "browser permission level: " + level;
    chrome.notifications.create("banner-test-" + Date.now(), {
      type: "basic",
      iconUrl: "icon128.png",
      title: "🔔 Banner test",
      message: "If you can read this on screen, banners work.",
      priority: 2,
      requireInteraction: false,
    }, (id) => {
      if (chrome.runtime.lastError) {
        msg += " | create FAILED: " + chrome.runtime.lastError.message;
      } else {
        msg += " | create ok (id " + id + ") — if no banner appeared, macOS or Focus is suppressing this browser";
      }
      diag.textContent = msg;
    });
  });
});
