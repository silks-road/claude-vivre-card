// background.js — service worker.
// Detection is NETWORK-level: claude.ai streams responses over
// /api/organizations/<org>/chat_conversations/<conv>/completion, and that
// request completes exactly when Claude finishes the turn. This is immune to
// UI redesigns and DOM virtualization (which broke selector-based detection).

const LISTENER_URL = "http://127.0.0.1:52741/event";

const COMPLETION_FILTER = {
  urls: [
    "https://claude.ai/api/organizations/*/chat_conversations/*/completion*",
    "https://claude.ai/api/organizations/*/chat_conversations/*/retry_completion*",
  ],
};

function conversationIdFromApiUrl(url) {
  const m = url.match(/chat_conversations\/([0-9a-f-]+)\//i);
  return m ? m[1] : "";
}

async function onTurnComplete(details) {
  const conversationId = conversationIdFromApiUrl(details.url);
  if (!conversationId || details.tabId < 0) return;

  // Let the final tokens render, then ask the page for title + last message.
  await new Promise((r) => setTimeout(r, 800));

  let info = { title: "", lastMessage: "" };
  try {
    info = await chrome.tabs.sendMessage(details.tabId, { type: "get_last_message" });
  } catch (e) {
    // Content script unavailable (tab closed / not reloaded) — fall back to tab title.
    try {
      const tab = await chrome.tabs.get(details.tabId);
      info.title = (tab.title || "").replace(/\s*[-–—]\s*Claude\s*$/i, "").trim();
    } catch (_) {}
  }

  const result = await forward({
    conversationId,
    title: info.title || "",
    lastMessage: info.lastMessage || "",
    url: "https://claude.ai/chat/" + conversationId,
  });

  // The listener classifies and returns the uniform title/body; the
  // notification is rendered HERE so its click is handled by THIS browser —
  // an OS-level "open URL" would go to the default browser, which may be a
  // different browser or a different Claude account.
  if (result && result.notify) {
    chrome.notifications.create("claude-chat:" + conversationId, {
      type: "basic",
      iconUrl: "icon128.png",
      title: result.title || "Claude",
      message: result.message || "",
      priority: 1,
    });
  }
}

// Notification click → focus the exact tab for that conversation (or open one
// here, never in another browser).
chrome.notifications.onClicked.addListener(async (notifId) => {
  if (!notifId.startsWith("claude-chat:")) return;
  const conversationPath = "/chat/" + notifId.slice("claude-chat:".length);
  chrome.notifications.clear(notifId);

  const tabs = await chrome.tabs.query({ url: "https://claude.ai/*" });
  const existing = tabs.find((t) => new URL(t.url).pathname === conversationPath);
  if (existing) {
    await chrome.tabs.update(existing.id, { active: true });
    await chrome.windows.update(existing.windowId, { focused: true });
  } else {
    const tab = await chrome.tabs.create({ url: "https://claude.ai" + conversationPath });
    await chrome.windows.update(tab.windowId, { focused: true });
  }
});

chrome.webRequest.onCompleted.addListener((d) => {
  // Visible heartbeat: flash the badge so detection is observable without DevTools.
  chrome.action.setBadgeText({ text: "✓" });
  chrome.action.setBadgeBackgroundColor({ color: "#178a3a" });
  setTimeout(() => chrome.action.setBadgeText({ text: "" }), 4000);
  onTurnComplete(d).catch((e) => {
    chrome.action.setBadgeText({ text: "E" });
    chrome.action.setBadgeBackgroundColor({ color: "#c0392b" });
  });
}, COMPLETION_FILTER);

// Multi-tab correctness: clicking a notification runs `open <chat url>`, which
// makes Chrome open a NEW tab even when that conversation is already open
// (possibly among several claude.ai tabs / split views). When a freshly created
// tab navigates to a chat URL another tab already shows, close the new tab and
// focus the existing one — the click always lands on the exact conversation.
const newTabIds = new Set();
chrome.tabs.onCreated.addListener((tab) => newTabIds.add(tab.id));

chrome.tabs.onUpdated.addListener(async (tabId, changeInfo, tab) => {
  if (!changeInfo.url || !newTabIds.has(tabId)) return;
  const m = changeInfo.url.match(/^https:\/\/claude\.ai\/chat\/([0-9a-f-]+)/i);
  if (!m) { newTabIds.delete(tabId); return; }
  const conversationPath = "/chat/" + m[1];

  const tabs = await chrome.tabs.query({ url: "https://claude.ai/*" });
  const existing = tabs.find(
    (t) => t.id !== tabId && new URL(t.url).pathname === conversationPath
  );
  newTabIds.delete(tabId);
  if (existing) {
    await chrome.tabs.remove(tabId);
    await chrome.tabs.update(existing.id, { active: true });
    await chrome.windows.update(existing.windowId, { focused: true });
  }
});
chrome.tabs.onRemoved.addListener((tabId) => newTabIds.delete(tabId));

async function forward(payload) {
  const { token } = await chrome.storage.local.get("token");
  if (!token) {
    chrome.action.setBadgeText({ text: "!" });
    chrome.action.setBadgeBackgroundColor({ color: "#D97757" });
    throw new Error("no token set — open the extension popup and paste it");
  }
  const resp = await fetch(LISTENER_URL, {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-Auth-Token": token },
    body: JSON.stringify(payload),
  });
  if (!resp.ok) throw new Error("listener returned " + resp.status);
  chrome.action.setBadgeText({ text: "" });
  return resp.json().catch(() => ({}));
}
