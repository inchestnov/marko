// Auto-sync page: opened directly by `marko sync --auto-open` at
// chrome-extension://<fixed-id>/sync/index.html?port=<port>&preview=<0|1>
// (the extension id is pinned via the "key" field in chrome/manifest.json
// so the CLI can construct this URL without any prior configuration).
//
// Unlike the popup, this page runs the whole connect -> diff ->
// apply-or-preview -> report flow automatically on load, with no button
// clicks required, so the CLI is the thing that "starts" a sync from the
// user's point of view. See docs/architecture.md §8/§10 and
// docs/sync-protocol.md.

import { useEffect, useState } from "react";
import DiffView from "../components/DiffView";
import { getPlan, postDiff, postReport } from "../lib/api";
import {
  applyOperations,
  chromeTreeToBookmarkTree,
  planOperations,
} from "../lib/bookmarksApply";
import type { Operation, OperationResult } from "../lib/types";

type Phase = "connecting" | "ready" | "applying" | "done" | "error";

function readParams(): { port: number; preview: boolean } {
  const params = new URLSearchParams(window.location.search);
  const portParam = Number(params.get("port"));
  const port = Number.isFinite(portParam) && portParam > 0 ? portParam : 8765;
  const preview = params.get("preview") === "1" || params.get("preview") === "true";
  return { port, preview };
}

export default function Sync() {
  const [{ port, preview }] = useState(readParams);
  const [phase, setPhase] = useState<Phase>("connecting");
  const [operations, setOperations] = useState<Operation[]>([]);
  const [results, setResults] = useState<OperationResult[]>([]);
  const [errorMessage, setErrorMessage] = useState<string | undefined>();

  useEffect(() => {
    let cancelled = false;

    async function run() {
      try {
        // GET /plan -> desiredTree; chrome.bookmarks.getTree() -> actualTree
        // (local); POST /diff -> operations[]. Same sequence as the popup's
        // manual "Connect", just triggered automatically on page load.
        await getPlan(port);
        const chromeRoots = await chrome.bookmarks.getTree();
        const actualTree = chromeTreeToBookmarkTree(chromeRoots);
        const diff = await postDiff(port, actualTree);
        if (cancelled) return;

        setOperations(diff.operations);
        setPhase(preview ? "done" : "ready");

        const opResults = preview
          ? planOperations(diff.operations)
          : await applyOperations(diff.operations);
        if (cancelled) return;

        setResults(opResults);
        await postReport(port, opResults, preview);
        if (cancelled) return;
        setPhase("done");
      } catch (err) {
        if (cancelled) return;
        setErrorMessage(err instanceof Error ? err.message : String(err));
        setPhase("error");
      }
    }

    run();
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const errorCount = results.filter((r) => r.status === "error").length;

  return (
    <div
      style={{
        maxWidth: 640,
        margin: "32px auto",
        fontFamily: '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif',
        padding: "0 16px",
        color: "#0f172a",
      }}
    >
      <h1 style={{ fontSize: 20, marginBottom: 4 }}>
        Marko {preview ? "Preview" : "Sync"}
      </h1>
      <p style={{ fontSize: 13, color: "#64748b", marginTop: 0 }}>
        Connected to marko sync on port {port}.
        {preview
          ? " Preview mode: nothing below was actually changed."
          : " Changes were applied to your bookmarks as they were computed."}
      </p>

      {phase === "connecting" && (
        <p style={{ fontSize: 13, color: "#334155" }}>Connecting and computing diff...</p>
      )}

      {phase === "applying" && (
        <p style={{ fontSize: 13, color: "#334155" }}>Applying changes...</p>
      )}

      {errorMessage && (
        <div
          style={{
            padding: "10px 12px",
            borderRadius: 6,
            background: "#fef2f2",
            border: "1px solid #fecaca",
            color: "#b91c1c",
            fontSize: 13,
            marginBottom: 12,
          }}
        >
          {errorMessage}
        </div>
      )}

      {(phase === "ready" || phase === "applying" || phase === "done") && (
        <DiffView operations={operations} />
      )}

      {phase === "done" && !errorMessage && (
        <div
          style={{
            marginTop: 16,
            padding: "10px 12px",
            borderRadius: 6,
            background: errorCount === 0 ? "#f0fdf4" : "#fffbeb",
            border: `1px solid ${errorCount === 0 ? "#bbf7d0" : "#fde68a"}`,
            color: errorCount === 0 ? "#166534" : "#92400e",
            fontSize: 13,
          }}
        >
          {preview
            ? `Preview complete: ${operations.length} operation(s) would run. Nothing was changed.`
            : `Sync complete: ${results.length - errorCount} ok, ${errorCount} error(s).`}{" "}
          The full log was sent back to the CLI. You can close this tab.
        </div>
      )}
    </div>
  );
}
