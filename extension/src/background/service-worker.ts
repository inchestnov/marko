// Background service worker (Manifest V3). Stateless across suspension;
// anything needed across popup/options open/close cycles is persisted via
// chrome.storage.local. See docs/architecture.md §10.1.
//
// Responsibilities are intentionally minimal: relay/persist the last-used
// port (so popup/options open with the same value already in place) and
// keep the toolbar badge in sync with the last known pending-operations
// count, itself only ever read from/written to chrome.storage.local — no
// long-lived in-memory state is held here.

const STORAGE_PORT_KEY = "markoPort";
const STORAGE_PENDING_COUNT_KEY = "markoPendingOperationsCount";
const DEFAULT_PORT = 8765;

/** Recomputes the toolbar badge from whatever is currently in storage. */
async function refreshBadge(): Promise<void> {
  const stored = await chrome.storage.local.get(STORAGE_PENDING_COUNT_KEY);
  const count = stored[STORAGE_PENDING_COUNT_KEY];

  if (typeof count === "number" && count > 0) {
    await chrome.action.setBadgeText({ text: String(count) });
    await chrome.action.setBadgeBackgroundColor({ color: "#2563eb" });
  } else {
    await chrome.action.setBadgeText({ text: "" });
  }
}

// On install, seed a default port so popup/options have a value to read
// even before the user ever opens either UI.
chrome.runtime.onInstalled.addListener(() => {
  chrome.storage.local.get(STORAGE_PORT_KEY).then((stored) => {
    if (typeof stored[STORAGE_PORT_KEY] !== "number") {
      chrome.storage.local.set({ [STORAGE_PORT_KEY]: DEFAULT_PORT });
    }
  });
  void refreshBadge();
});

// The service worker can be suspended and woken at any time; re-derive the
// badge from storage whenever it wakes, rather than relying on any
// in-memory value surviving.
chrome.runtime.onStartup.addListener(() => {
  void refreshBadge();
});

// Popup/options write the pending-operations count directly to
// chrome.storage.local after each /diff response; react to that change here
// so the badge stays in sync regardless of which page made the write.
chrome.storage.onChanged.addListener((changes, areaName) => {
  if (areaName !== "local") return;
  if (STORAGE_PENDING_COUNT_KEY in changes) {
    void refreshBadge();
  }
});
