// A single operation row: icon + path + before/after. See
// docs/architecture.md §10.2.

import type { Operation } from "../lib/types";

const OP_STYLES: Record<Operation["type"], { icon: string; color: string; label: string }> = {
  CREATE: { icon: "+", color: "#16a34a", label: "Create" },
  UPDATE: { icon: "~", color: "#2563eb", label: "Update" },
  DELETE: { icon: "-", color: "#dc2626", label: "Delete" },
  MOVE: { icon: "→", color: "#9333ea", label: "Move" },
};

export interface OperationRowProps {
  operation: Operation;
}

export default function OperationRow({ operation }: OperationRowProps) {
  const style = OP_STYLES[operation.type];
  const pathStr = operation.targetPath.join(" / ");

  return (
    <div
      style={{
        display: "flex",
        alignItems: "flex-start",
        gap: 10,
        padding: "8px 10px",
        borderBottom: "1px solid #f1f5f9",
        fontSize: 13,
      }}
    >
      <span
        aria-hidden
        style={{
          display: "inline-flex",
          alignItems: "center",
          justifyContent: "center",
          width: 18,
          height: 18,
          borderRadius: 4,
          background: `${style.color}1a`,
          color: style.color,
          fontWeight: 700,
          flexShrink: 0,
          fontFamily: "monospace",
        }}
      >
        {style.icon}
      </span>
      <div style={{ flexGrow: 1, minWidth: 0 }}>
        <div style={{ display: "flex", gap: 6, alignItems: "baseline" }}>
          <span style={{ fontWeight: 600, color: "#0f172a" }}>
            {operation.kind === "folder" ? "Folder" : "Bookmark"}
          </span>
          <span
            style={{
              color: "#475569",
              overflow: "hidden",
              textOverflow: "ellipsis",
              whiteSpace: "nowrap",
            }}
            title={pathStr}
          >
            {pathStr}
          </span>
        </div>
        {operation.type === "UPDATE" && operation.changes?.length ? (
          <div style={{ color: "#64748b", marginTop: 2 }}>
            {operation.changes.includes("name") && (
              <div>
                name: <em>{operation.name}</em>
              </div>
            )}
            {operation.changes.includes("url") && (
              <div>
                url: <em>{operation.url}</em>
              </div>
            )}
          </div>
        ) : operation.kind === "bookmark" && operation.url ? (
          <div
            style={{
              color: "#94a3b8",
              overflow: "hidden",
              textOverflow: "ellipsis",
              whiteSpace: "nowrap",
            }}
            title={operation.url}
          >
            {operation.url}
          </div>
        ) : null}
      </div>
    </div>
  );
}
