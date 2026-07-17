// content.js — runs on claude.ai. Detects when Claude finishes a turn by
// watching the "Stop response" button: present while streaming, gone when done.
// On the streaming→idle transition it captures the conversation id, title, and
// last assistant message, and asks the service worker to notify.

(function () {
  "use strict";

  const STOP_SELECTOR = 'button[aria-label="Stop response"]';

  // True while Claude is actively responding. Two signals, either suffices:
  // 1. A "Stop response" button (present in some UI variants).
  // 2. Structural: every ANSWERED user message has an action bar (retry etc.)
  //    on its response; while a response is still streaming, the newest user
  //    message has no action bar yet — so pending = users - actionBars > 0.
  function isStreaming() {
    if (document.querySelector(STOP_SELECTOR)) return true;
    const users = document.querySelectorAll('[data-testid="user-message"]').length;
    const bars = document.querySelectorAll('[data-testid="action-bar-retry"]').length;
    return users > bars;
  }

  function conversationId() {
    const m = location.pathname.match(/\/chat\/([0-9a-f-]+)/i);
    return m ? m[1] : "";
  }

  function conversationTitle() {
    // Tab title is "<chat name> - Claude"; strip the suffix.
    return (document.title || "").replace(/\s*[-–—]\s*Claude\s*$/i, "").trim();
  }

  // Grab the text of the last assistant message. claude.ai renders assistant
  // turns in containers; we take the last one that isn't the user's own.
  function lastAssistantText() {
    const userMsgs = document.querySelectorAll('[data-testid="user-message"]');
    // Assistant messages sit between user messages; the response content is the
    // last large text block on the page. Fall back to broad selectors.
    const candidates = document.querySelectorAll(
      '[data-testid="user-message"] ~ div, .font-claude-message, [class*="prose"]'
    );
    let text = "";
    for (const el of candidates) {
      const t = (el.innerText || "").trim();
      if (t) text = t;
    }
    // Last resort: whole main region minus nothing — cap length.
    if (!text) {
      const main = document.querySelector("main");
      if (main) text = (main.innerText || "").trim();
    }
    return text.slice(0, 2000);
  }

  let streaming = false;
  let sawStreamingForThisTurn = false;

  function check() {
    const now = isStreaming();
    if (now && !streaming) {
      // streaming just started
      streaming = true;
      sawStreamingForThisTurn = true;
    } else if (!now && streaming) {
      // streaming just ended → turn complete
      streaming = false;
      if (sawStreamingForThisTurn) {
        sawStreamingForThisTurn = false;
        // Small delay so the final tokens/DOM settle.
        setTimeout(reportTurnComplete, 600);
      }
    }
  }

  function reportTurnComplete() {
    const id = conversationId();
    if (!id) return; // not in a chat
    const payload = {
      conversationId: id,
      title: conversationTitle(),
      lastMessage: lastAssistantText(),
      url: location.origin + "/chat/" + id,
    };
    chrome.runtime.sendMessage({ type: "turn_complete", payload });
  }

  // Observe DOM mutations (streaming toggles the Stop button in/out).
  const observer = new MutationObserver(() => check());
  observer.observe(document.body, { childList: true, subtree: true });

  // Also poll as a safety net (some updates don't bubble as mutations).
  setInterval(check, 1000);
})();
