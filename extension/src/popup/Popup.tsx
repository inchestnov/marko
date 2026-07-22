// Main popup UI. See docs/architecture.md §10.2.
// Wires up ConnectionStatus, DiffView, and the Connect/Apply flows via
// lib/api.ts and lib/bookmarksApply.ts.

import { useState } from "react";
import ConnectionStatus from "../components/ConnectionStatus";
import DiffView from "../components/DiffView";
import { getPlan, postDiff, postReport } from "../lib/api";
import { applyOperations, chromeTreeToBookmarkTree } from "../lib/bookmarksApply";
import type { Operation, OperationResult } from "../lib/types";

type Phase = "idle" | "connecting" | "ready" | "applying" | "done" | "error";

export default function Popup() {
  const [port, setPort] = useState<number>(8765);
  const [connected, setConnected] = useState(false);
  const [phase, setPhase] = useState<Phase>("idle");
  const [operations, setOperations] = useState<Operation[]>([]);
  const [errorMessage, setErrorMessage] = useState<string | undefined>();
  const [report, setReport] = useState<{ okCount: number; errorCount: number } | undefined>();

  async function handleConnect() {
    setPhase("connecting");
    setErrorMessage(undefined);
    setReport(undefined);
    try {
      // GET /plan -> desiredTree (the CLI has no way to read the browser
      // itself; the extension supplies actualTree separately, see §8.2/8.3).
      await getPlan(port);

      // chrome.bookmarks.getTree() -> actualTree (local).
      const chromeRoots = await chrome.bookmarks.getTree();
      const actualTree = chromeTreeToBookmarkTree(chromeRoots);

      // POST /diff { actualTree } -> operations[].
      const diff = await postDiff(port, actualTree);

      setOperations(diff.operations);
      setPhase("ready");
      await chrome.storage.local.set({
        markoPendingOperationsCount: diff.operations.length,
      });
    } catch (err) {
      setErrorMessage(err instanceof Error ? err.message : String(err));
      setPhase("error");
    }
  }

  async function handleApply() {
    setPhase("applying");
    setErrorMessage(undefined);
    try {
      const results: OperationResult[] = await applyOperations(operations);
      const okCount = results.filter((r) => r.status === "ok").length;
      const errorCount = results.filter((r) => r.status === "error").length;

      await postReport(port, results);

      setReport({ okCount, errorCount });
      setPhase("done");
      await chrome.storage.local.set({ markoPendingOperationsCount: errorCount });
    } catch (err) {
      setErrorMessage(err instanceof Error ? err.message : String(err));
      setPhase("error");
    }
  }

  return (
    <div
      style={{
        width: 420,
        minHeight: 200,
        fontFamily:
          '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif',
        padding: 14,
        boxSizing: "border-box",
        background: "#ffffff",
      }}
    >
      <div style={{ display: "flex", alignItems: "center", marginBottom: 10 }}>
        <h1 style={{ fontSize: 15, margin: 0, color: "#0f172a", flexGrow: 1 }}>Marko</h1>
      </div>

      <ConnectionStatus onPortChange={setPort} onStatusChange={setConnected} />

      <div style={{ display: "flex", gap: 8, marginTop: 12 }}>
        <button
          onClick={handleConnect}
          disabled={!connected || phase === "connecting" || phase === "applying"}
          style={buttonStyle(connected)}
        >
          {phase === "connecting" ? "Connecting..." : "Connect"}
        </button>
        <button
          onClick={handleApply}
          disabled={phase !== "ready" || operations.length === 0}
          style={buttonStyle(phase === "ready" && operations.length > 0, "#16a34a")}
        >
          {phase === "applying" ? "Applying..." : "Apply"}
        </button>
      </div>

      {errorMessage && (
        <div
          style={{
            marginTop: 12,
            padding: "8px 10px",
            borderRadius: 6,
            background: "#fef2f2",
            border: "1px solid #fecaca",
            color: "#b91c1c",
            fontSize: 12,
          }}
        >
          {errorMessage}
        </div>
      )}

      {report && (
        <div
          style={{
            marginTop: 12,
            padding: "8px 10px",
            borderRadius: 6,
            background: report.errorCount === 0 ? "#f0fdf4" : "#fffbeb",
            border: `1px solid ${report.errorCount === 0 ? "#bbf7d0" : "#fde68a"}`,
            color: report.errorCount === 0 ? "#166534" : "#92400e",
            fontSize: 12,
          }}
        >
          Applied {report.okCount} operation{report.okCount === 1 ? "" : "s"} successfully
          {report.errorCount > 0 ? `, ${report.errorCount} failed.` : "."}
        </div>
      )}

      {(phase === "ready" || phase === "applying" || phase === "done") && (
        <div style={{ marginTop: 12, maxHeight: 320, overflowY: "auto" }}>
          <DiffView operations={operations} />
        </div>
      )}
    </div>
  );
}

function buttonStyle(enabled: boolean, color = "#2563eb"): React.CSSProperties {
  return {
    flex: 1,
    padding: "8px 12px",
    borderRadius: 6,
    border: "none",
    fontSize: 13,
    fontWeight: 600,
    cursor: enabled ? "pointer" : "not-allowed",
    background: enabled ? color : "#cbd5e1",
    color: "#ffffff",
    transition: "background 120ms ease",
  };
}
