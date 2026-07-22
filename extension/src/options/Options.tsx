// Options page UI: default port configuration, export/import current
// browser state. See docs/architecture.md §10.3.

import { useEffect, useRef, useState } from "react";
import { DEFAULT_PORT, STORAGE_PORT_KEY } from "../lib/api";
import { chromeTreeToBookmarkTree } from "../lib/bookmarksApply";
import { importFromExportFile } from "../lib/importExport";
import type { ExportFile } from "../lib/types";

type Message = { kind: "success" | "error"; text: string };

export default function Options() {
  const [port, setPort] = useState<number>(DEFAULT_PORT);
  const [saved, setSaved] = useState(false);
  const [message, setMessage] = useState<Message | undefined>();
  const [importing, setImporting] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    chrome.storage.local.get(STORAGE_PORT_KEY).then((stored) => {
      const storedPort = stored[STORAGE_PORT_KEY];
      if (typeof storedPort === "number" && storedPort > 0) {
        setPort(storedPort);
      }
    });
  }, []);

  function handlePortChange(e: React.ChangeEvent<HTMLInputElement>) {
    const next = Number(e.target.value);
    if (!Number.isFinite(next) || next <= 0) return;
    setPort(next);
    setSaved(false);
  }

  async function handleSavePort() {
    await chrome.storage.local.set({ [STORAGE_PORT_KEY]: port });
    setSaved(true);
  }

  async function handleExport() {
    setMessage(undefined);
    try {
      const chromeRoots = await chrome.bookmarks.getTree();
      const actualTree = chromeTreeToBookmarkTree(chromeRoots);
      const json = JSON.stringify(actualTree, null, 2);
      const blob = new Blob([json], { type: "application/json" });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `marko-browser-state-${new Date().toISOString().slice(0, 10)}.json`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
      setMessage({ kind: "success", text: "Exported current browser state." });
    } catch (err) {
      setMessage({
        kind: "error",
        text: `Export failed: ${err instanceof Error ? err.message : String(err)}`,
      });
    }
  }

  function handleImportClick() {
    fileInputRef.current?.click();
  }

  async function handleFileSelected(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    e.target.value = ""; // allow re-selecting the same file later
    if (!file) return;

    setImporting(true);
    setMessage(undefined);
    try {
      const text = await file.text();
      const parsed = JSON.parse(text) as ExportFile;
      if (!parsed.desiredTree || !Array.isArray(parsed.desiredTree.roots)) {
        throw new Error("File does not look like a marko export (missing desiredTree.roots)");
      }

      const chromeRoots = await chrome.bookmarks.getTree();
      const actualTree = chromeTreeToBookmarkTree(chromeRoots);

      const createdCount = await importFromExportFile(parsed.desiredTree, actualTree);
      setMessage({
        kind: "success",
        text: `Import complete: created ${createdCount} node${createdCount === 1 ? "" : "s"} (CREATE-only, nothing deleted or moved).`,
      });
    } catch (err) {
      setMessage({
        kind: "error",
        text: `Import failed: ${err instanceof Error ? err.message : String(err)}`,
      });
    } finally {
      setImporting(false);
    }
  }

  return (
    <div
      style={{
        maxWidth: 560,
        margin: "40px auto",
        fontFamily: '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif',
        color: "#0f172a",
        padding: "0 20px",
      }}
    >
      <h1 style={{ fontSize: 20, marginBottom: 4 }}>Marko Options</h1>
      <p style={{ color: "#64748b", fontSize: 13, marginTop: 0 }}>
        Configure the local marko sync connection and manage offline export/import.
      </p>

      <section
        style={{
          border: "1px solid #e2e8f0",
          borderRadius: 8,
          padding: 16,
          marginTop: 20,
        }}
      >
        <h2 style={{ fontSize: 15, marginTop: 0 }}>Connection</h2>
        <label style={{ display: "flex", alignItems: "center", gap: 10, fontSize: 13 }}>
          Default port
          <input
            type="number"
            value={port}
            onChange={handlePortChange}
            style={{
              width: 90,
              padding: "5px 8px",
              border: "1px solid #cbd5e1",
              borderRadius: 4,
              fontSize: 13,
            }}
          />
          <button onClick={handleSavePort} style={secondaryButtonStyle}>
            Save
          </button>
          {saved && <span style={{ color: "#16a34a", fontSize: 12 }}>Saved</span>}
        </label>
      </section>

      <section
        style={{
          border: "1px solid #e2e8f0",
          borderRadius: 8,
          padding: 16,
          marginTop: 16,
        }}
      >
        <h2 style={{ fontSize: 15, marginTop: 0 }}>Offline export / import</h2>
        <p style={{ color: "#64748b", fontSize: 13 }}>
          Export the current browser bookmark state as JSON for use with{" "}
          <code>marko diff --actual</code>, or import a <code>marko export</code> file using a
          conservative CREATE-only, skip-existing fallback (never deletes or moves bookmarks).
        </p>
        <div style={{ display: "flex", gap: 8 }}>
          <button onClick={handleExport} style={primaryButtonStyle}>
            Export current browser state
          </button>
          <button onClick={handleImportClick} disabled={importing} style={secondaryButtonStyle}>
            {importing ? "Importing..." : "Import from file"}
          </button>
          <input
            ref={fileInputRef}
            type="file"
            accept="application/json,.json"
            onChange={handleFileSelected}
            style={{ display: "none" }}
          />
        </div>
      </section>

      {message && (
        <div
          style={{
            marginTop: 16,
            padding: "10px 12px",
            borderRadius: 6,
            fontSize: 13,
            background: message.kind === "success" ? "#f0fdf4" : "#fef2f2",
            border: `1px solid ${message.kind === "success" ? "#bbf7d0" : "#fecaca"}`,
            color: message.kind === "success" ? "#166534" : "#b91c1c",
          }}
        >
          {message.text}
        </div>
      )}
    </div>
  );
}

const primaryButtonStyle: React.CSSProperties = {
  padding: "8px 14px",
  borderRadius: 6,
  border: "none",
  background: "#2563eb",
  color: "#fff",
  fontSize: 13,
  fontWeight: 600,
  cursor: "pointer",
};

const secondaryButtonStyle: React.CSSProperties = {
  padding: "8px 14px",
  borderRadius: 6,
  border: "1px solid #cbd5e1",
  background: "#fff",
  color: "#0f172a",
  fontSize: 13,
  fontWeight: 600,
  cursor: "pointer",
};
