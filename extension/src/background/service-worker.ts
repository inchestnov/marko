// Background service worker (Manifest V3). Stateless across suspension;
// anything needed across popup/options open/close cycles is persisted via
// chrome.storage.local. See docs/architecture.md §10.1.
//
// TODO(extension-agent): relay connection state between popup/options and
// chrome.storage.local, optionally show a badge count of pending
// operations.

export {};
